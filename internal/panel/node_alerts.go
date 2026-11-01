package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type nodeAlertState struct {
	Notified bool  `json:"notified"`
	RetryAt  int64 `json:"retry_at"`
}

type alertNode struct {
	id         int64
	name       string
	lastSample int64
	created    int64
}

func nodeSampleStale(now, lastSample, created int64, timeout time.Duration) bool {
	if lastSample == 0 {
		lastSample = created
	}
	return lastSample > 0 && now-lastSample >= timeout.Milliseconds()
}

func (a *App) runNodeAlerts(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkNodeAlerts(ctx)
		}
	}
}

func (a *App) checkNodeAlerts(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	// Read a private settings snapshot: notification edits must not race this worker.
	cfg := Config{NodeAlertEnabled: true, NodeAlertTimeoutSeconds: 120, SMTPPort: 587}
	if err := a.settings.applyToConfig(ctx, &cfg); err != nil {
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT n.id,n.name,
	 (SELECT MAX(m.sampled_at_ms) FROM node_system_metrics m WHERE m.node_id=n.id),
	 UNIX_TIMESTAMP(COALESCE(g.installed_at,g.created_at))*1000
	 FROM server_nodes n JOIN node_agents g ON g.node_id=n.id
	 WHERE n.deleted_at IS NULL AND n.agent_enabled=1`)
	if err != nil {
		a.logger.Warnf("节点异常检查暂时无法完成")
		return
	}
	nodes := []alertNode{}
	for rows.Next() {
		var n alertNode
		var sample sql.NullInt64
		if err = rows.Scan(&n.id, &n.name, &sample, &n.created); err != nil {
			break
		}
		n.lastSample = sample.Int64
		nodes = append(nodes, n)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		return
	}
	for _, n := range nodes {
		if parent.Err() != nil {
			return
		}
		nodeCtx, nodeCancel := context.WithTimeout(parent, 40*time.Second)
		a.checkNodeAlert(nodeCtx, cfg, n, sendSMTPMail)
		nodeCancel()
	}
}

func (a *App) checkNodeAlert(ctx context.Context, cfg Config, n alertNode, send func(Config, string, string, string) error) {
	key := "node_sample_alert_state_" + strconv.FormatInt(n.id, 10)
	raw, exists, err := a.settings.get(ctx, key)
	if err != nil {
		return
	}
	var state nodeAlertState
	if exists && json.Unmarshal([]byte(raw), &state) != nil {
		return
	}
	next, changed, sendErr := advanceNodeAlert(cfg, n, state, time.Now(), send)
	if sendErr != nil {
		a.logger.Warnf("节点异常通知发送失败，将稍后重试")
	}
	if !changed {
		return
	}
	state = next
	data, _ := json.Marshal(state)
	if err := a.settings.set(ctx, key, string(data)); err != nil {
		a.logger.Warnf("节点通知状态暂时无法保存")
	}
}

func advanceNodeAlert(cfg Config, n alertNode, state nodeAlertState, now time.Time, send func(Config, string, string, string) error) (nodeAlertState, bool, error) {
	stale := nodeSampleStale(now.UnixMilli(), n.lastSample, n.created, time.Duration(cfg.NodeAlertTimeoutSeconds)*time.Second)
	if !stale {
		return nodeAlertState{}, state != (nodeAlertState{}), nil
	}
	if !cfg.NodeAlertEnabled || !cfg.SMTPEnabled || strings.TrimSpace(cfg.SMTPNotifyEmail) == "" || state.Notified || now.Unix() < state.RetryAt {
		return state, false, nil
	}
	body := fmt.Sprintf("节点「%s」（编号 %d）已连续至少 %d 秒未收到有效采样。\n请检查节点网络、Agent 和服务运行状态。\n检测时间：%s", n.name, n.id, cfg.NodeAlertTimeoutSeconds, now.Format(time.RFC3339))
	if err := send(cfg, cfg.SMTPNotifyEmail, "Hysteria2MX 节点采样异常", body); err != nil {
		state.RetryAt = now.Add(5 * time.Minute).Unix()
		return state, true, err
	}
	return nodeAlertState{Notified: true}, true, nil
}
