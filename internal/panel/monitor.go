package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"mxinhy/internal/nodemonitor"
	"net/http"
	"strconv"
	"time"
)

type monitorPoint struct {
	SampledAtMS      int64    `json:"sampled_at_ms"`
	CPUPercent       *float64 `json:"cpu_percent"`
	CPUPeak          *float64 `json:"cpu_peak"`
	MemoryPercent    float64  `json:"memory_percent"`
	SwapPercent      float64  `json:"swap_percent"`
	DiskPercent      float64  `json:"disk_percent"`
	Load1            float64  `json:"load1"`
	RXBytesPerSecond *float64 `json:"rx_bytes_per_second"`
	TXBytesPerSecond *float64 `json:"tx_bytes_per_second"`
}

func (a *App) handleMonitorNodes(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(r.Context(), a.auth, w, r)
	if !ok || !requirePermission(user, "node.view", w) {
		return
	}
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,name,host,agent_enabled,obfs_enabled FROM server_nodes WHERE deleted_at IS NULL ORDER BY name,id`)
	if e != nil {
		writeError(w, http.StatusInternalServerError, "节点列表暂时无法读取")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name, host string
		var enabled, obfs bool
		if rows.Scan(&id, &name, &host, &enabled, &obfs) != nil {
			writeError(w, http.StatusInternalServerError, "节点列表暂时无法读取")
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "host": host, "agent_enabled": enabled, "obfs_enabled": obfs})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "节点列表暂时无法读取")
		return
	}
	a.writeJSON(w, http.StatusOK, apiEnvelope{Success: true, Data: items})
}

func monitorWindow(value string) (time.Duration, int64) {
	switch value {
	case "6h":
		return 6 * time.Hour, 30000
	case "24h":
		return 24 * time.Hour, 120000
	case "7d":
		return 7 * 24 * time.Hour, 900000
	default:
		return time.Hour, 5000
	}
}

func (a *App) handleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	_, raw, ok := a.readAgentPayload(w, r)
	if !ok {
		return
	}
	record, e := a.agents.authenticateRequest(r.Context(), r, raw)
	if e != nil {
		writeError(w, http.StatusUnauthorized, "监控上报认证失败")
		return
	}
	if boolValue(record["node_deleted"], false) {
		a.writeAgentNodeDeleted(w, r, record, "监控上报时发现节点已删除")
		return
	}
	var m nodemonitor.Metrics
	if e = json.Unmarshal([]byte(raw), &m); e != nil || m.Validate(time.Now()) != nil {
		writeError(w, http.StatusBadRequest, "监控数据格式无效")
		return
	}
	cleanPayload, _ := json.Marshal(m)
	_, e = a.db.ExecContext(r.Context(), `INSERT INTO node_system_metrics
	(node_id,sampled_at_ms,cpu_percent,memory_percent,swap_percent,disk_percent,load1,rx_rate,tx_rate,payload_json)
	VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE sampled_at_ms=VALUES(sampled_at_ms)`,
		int64Value(record["node_id"]), m.SampledAtMS, m.CPUPercent, m.MemoryPercent, m.SwapPercent, m.DiskPercent, m.Load1, m.RXBytesPerSecond, m.TXBytesPerSecond, string(cleanPayload))
	if e != nil {
		writeError(w, http.StatusInternalServerError, "监控数据暂时无法保存")
		return
	}
	a.writeJSON(w, http.StatusOK, apiEnvelope{Success: true, Data: map[string]bool{"ok": true}})
}

func (a *App) handleNodeMonitor(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(r.Context(), a.auth, w, r)
	if !ok || !requirePermission(user, "node.view", w) {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	node, e := a.hysteria.getStoredNode(r.Context(), id)
	if e != nil || node == nil || node.DeletedAt.Valid {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	window, bucket := monitorWindow(r.URL.Query().Get("range"))
	now := time.Now()
	from := now.Add(-window).UnixMilli()
	rows, e := a.db.QueryContext(ctx, `SELECT FLOOR(sampled_at_ms/?)*?,AVG(cpu_percent),MAX(cpu_percent),AVG(memory_percent),AVG(swap_percent),AVG(disk_percent),AVG(load1),AVG(rx_rate),AVG(tx_rate)
	FROM node_system_metrics WHERE node_id=? AND sampled_at_ms>=? AND sampled_at_ms<=?
	GROUP BY FLOOR(sampled_at_ms/?) ORDER BY 1 LIMIT 1500`, bucket, bucket, id, from, now.UnixMilli(), bucket)
	if e != nil {
		writeError(w, http.StatusInternalServerError, "监控记录暂时无法读取")
		return
	}
	points := make([]monitorPoint, 0)
	for rows.Next() {
		var p monitorPoint
		if e = rows.Scan(&p.SampledAtMS, &p.CPUPercent, &p.CPUPeak, &p.MemoryPercent, &p.SwapPercent, &p.DiskPercent, &p.Load1, &p.RXBytesPerSecond, &p.TXBytesPerSecond); e != nil {
			break
		}
		points = append(points, p)
	}
	rowErr := rows.Err()
	rows.Close()
	if e != nil || rowErr != nil {
		writeError(w, http.StatusInternalServerError, "监控记录暂时无法读取")
		return
	}
	var raw string
	var latest *nodemonitor.Metrics
	e = a.db.QueryRowContext(ctx, `SELECT payload_json FROM node_system_metrics WHERE node_id=? ORDER BY sampled_at_ms DESC LIMIT 1`, id).Scan(&raw)
	if e == nil {
		latest = &nodemonitor.Metrics{}
		if json.Unmarshal([]byte(raw), latest) != nil {
			latest = nil
		}
	} else if e != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "监控记录暂时无法读取")
		return
	}
	agent, e := a.agents.getAgentRecordByNodeID(ctx, id)
	if e != nil {
		writeError(w, http.StatusInternalServerError, "节点状态暂时无法读取")
		return
	}
	fresh := latest != nil && latest.SampledAtMS >= now.Add(-20*time.Second).UnixMilli() && a.agents.isRecordOnline(agent)
	a.writeJSON(w, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"node_id": id, "latest": latest, "points": points, "fresh": fresh, "agent": a.agents.presentAgent(agent), "from_ms": from, "to_ms": now.UnixMilli(), "bucket_ms": bucket, "retention_days": 7}})
}

func (a *App) startMonitorCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		// Bound each batch so maintenance cannot hold the history table for long.
		_, _ = a.db.ExecContext(cleanupCtx, `DELETE FROM node_system_metrics WHERE sampled_at_ms<? LIMIT 50000`, time.Now().Add(-7*24*time.Hour).UnixMilli())
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
