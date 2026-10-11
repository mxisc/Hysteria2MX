package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"mxinhy/internal/cpuprotection"
	"net/http"
	"strconv"
)

func cpuProtectionKey(nodeID int64) string { return fmt.Sprintf("node_cpu_protection_%d", nodeID) }

func (s *agentService) cpuProtection(ctx context.Context, nodeID int64) (*cpuprotection.Config, error) {
	value, found, e := newSystemSettingsStore(s.db).get(ctx, cpuProtectionKey(nodeID))
	if e != nil || !found {
		return nil, e
	}
	var cfg cpuprotection.Config
	if e = json.Unmarshal([]byte(value), &cfg); e != nil {
		return nil, e
	}
	cfg, e = cpuprotection.Normalize(cfg)
	if e != nil {
		return nil, e
	}
	return &cfg, nil
}

func (a *App) handleCPUProtection(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok {
		return
	}
	permission := "node.view"
	if request.Method == http.MethodPut {
		permission = "node.manage"
	}
	if !requirePermission(user, permission, writer) {
		return
	}
	id, e := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		writeError(writer, http.StatusNotFound, "节点不存在")
		return
	}
	node, e := a.hysteria.getStoredNode(request.Context(), id)
	if e != nil || node == nil || node.DeletedAt.Valid {
		writeError(writer, http.StatusNotFound, "节点不存在")
		return
	}
	if request.Method == http.MethodPut {
		var cfg cpuprotection.Config
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&cfg); e != nil {
			writeError(writer, http.StatusBadRequest, "CPU保护配置格式无效")
			return
		}
		cfg, e = cpuprotection.Normalize(cfg)
		if e != nil {
			writeError(writer, http.StatusBadRequest, e.Error())
			return
		}
		if cfg.Enabled && !node.AgentEnabled {
			writeError(writer, http.StatusBadRequest, "请先启用节点Agent，再开启CPU保护")
			return
		}
		data, _ := json.Marshal(cfg)
		if e = newSystemSettingsStore(a.db).set(request.Context(), cpuProtectionKey(id), string(data)); e != nil {
			writeError(writer, http.StatusInternalServerError, "CPU保护配置保存失败")
			return
		}
		a.logAudit(request.Context(), user, request, "node.cpu_protection.update", "node", strconv.FormatInt(id, 10), map[string]any{"enabled": cfg.Enabled, "high_percent": cfg.HighPercent})
	}
	cfg, e := a.agents.cpuProtection(request.Context(), id)
	if e != nil {
		writeError(writer, http.StatusInternalServerError, "CPU保护配置读取失败")
		return
	}
	configured := cfg != nil
	if cfg == nil {
		v, _ := cpuprotection.Normalize(cpuprotection.Config{})
		cfg = &v
	}
	applied := false
	record, e := a.agents.getAgentRecordByNodeID(request.Context(), id)
	if e == nil && record != nil && a.agents.isRecordOnline(record) {
		payload := mapValue(record["last_payload_json"])
		if raw, exists := payload["cpu_protection"]; exists {
			b, _ := json.Marshal(raw)
			var actual cpuprotection.Config
			if json.Unmarshal(b, &actual) == nil {
				actual, e = cpuprotection.Normalize(actual)
				applied = e == nil && actual == *cfg
			}
		}
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"config": cfg, "configured": configured, "applied": configured && applied}})
}
