package panel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	agentStatusPending = "pending"
	agentStatusOnline  = "online"
	agentStatusOffline = "offline"
	agentStatusError   = "error"

	taskStatusPending = "pending"
	taskStatusRunning = "running"
	taskStatusDone    = "done"
	taskStatusError   = "error"

	opcodeRestartHY2       = 1
	opcodeStopHY2          = 2
	opcodeStartHY2         = 3
	opcodeStatusHY2        = 4
	opcodeFetchLogs        = 5
	opcodeFetchOnline      = 6
	opcodeFetchStreams     = 7
	opcodeFetchUserTraffic = 8
	opcodeSyncTrafficClear = 9
	opcodeWriteConfig      = 10
	opcodeRestartAgent     = 11
	opcodeKickClients      = 12
	opcodeSyncLocalAuth    = 13

	agentRequestToleranceSeconds = 300
	agentOfflineGraceMultiplier  = 3
	agentDownloadTokenMaxTTL     = 900
)

type agentService struct {
	db  *sql.DB
	cfg *Config
}

func newAgentService(db *sql.DB, cfg *Config) *agentService {
	return &agentService{db: db, cfg: cfg}
}

func (s *agentService) ensureAgentRecordForNode(ctx context.Context, node nodeRecord) (map[string]any, error) {
	existing, err := s.getAgentRecordByNodeID(ctx, node.ID)
	if err != nil {
		return nil, err
	}

	reportInterval := maxInt(node.AgentReportIntervalSeconds, 2)
	taskPollInterval := maxInt(node.AgentTaskPollIntervalSeconds, 1)
	if existing != nil {
		_, err = s.db.ExecContext(ctx, `
			UPDATE node_agents
			SET report_interval_seconds = ?, task_poll_interval_seconds = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, reportInterval, taskPollInterval, existing["id"])
		if err != nil {
			return nil, err
		}
		return s.getAgentRecordByNodeID(ctx, node.ID)
	}

	agentID, err := randomHex(8)
	if err != nil {
		return nil, errors.New("Agent 记录创建失败")
	}
	secret, err := randomHex(32)
	if err != nil {
		return nil, errors.New("Agent 记录创建失败")
	}
	encryptedSecret, err := EncryptValue(secret, *s.cfg)
	if err != nil {
		return nil, errors.New("Agent 凭据保存失败")
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO node_agents (node_id, agent_id, shared_secret, status, report_interval_seconds, task_poll_interval_seconds)
		VALUES (?, ?, ?, ?, ?, ?)`,
		node.ID, "agent_"+agentID, encryptedSecret, agentStatusPending, reportInterval, taskPollInterval)
	if err != nil {
		return nil, err
	}
	return s.getAgentRecordByNodeID(ctx, node.ID)
}

func (s *agentService) getAgentRecordByNodeID(ctx context.Context, nodeID int64) (map[string]any, error) {
	if nodeID < 1 {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT * FROM node_agents WHERE node_id = ? LIMIT 1`, nodeID)
	record, err := s.scanAgentRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func (s *agentService) getAgentRecordByAgentID(ctx context.Context, agentID string) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `SELECT * FROM node_agents WHERE agent_id = ? LIMIT 1`, agentID)
	record, err := s.scanAgentRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func (s *agentService) effectiveStatus(record map[string]any) string {
	if record == nil {
		return agentStatusOffline
	}
	status := toString(record["status"])
	lastSeenText := toString(record["last_seen_at"])
	reportInterval := maxInt(intValue(record["report_interval_seconds"]), 2)
	if lastSeenText != "" {
		if lastSeen, err := time.Parse("2006-01-02 15:04:05", lastSeenText); err == nil {
			if lastSeen.Before(time.Now().Add(-time.Duration(reportInterval*agentOfflineGraceMultiplier) * time.Second)) {
				status = agentStatusOffline
			}
		}
	} else if status == agentStatusOnline {
		status = agentStatusOffline
	}
	return status
}

func (s *agentService) isRecordOnline(record map[string]any) bool {
	return s.effectiveStatus(record) == agentStatusOnline
}

func (s *agentService) presentAgent(record map[string]any) map[string]any {
	if record == nil {
		return nil
	}
	return map[string]any{
		"agent_id":                   record["agent_id"],
		"status":                     s.effectiveStatus(record),
		"version":                    record["version"],
		"report_interval_seconds":    record["report_interval_seconds"],
		"task_poll_interval_seconds": record["task_poll_interval_seconds"],
		"installed_at":               record["installed_at"],
		"last_seen_at":               record["last_seen_at"],
		"last_ip":                    record["last_ip"],
		"last_error":                 record["last_error"],
		"last_service_status":        record["last_service_status"],
		"last_service_message":       record["last_service_message"],
		"last_total_rx":              record["last_total_rx"],
		"last_total_tx":              record["last_total_tx"],
		"last_user_count":            record["last_user_count"],
		"last_online_count":          record["last_online_count"],
	}
}

func (s *agentService) buildBootstrapConfig(ctx context.Context, node nodeRecord, panelAPIBaseURL string) (map[string]any, error) {
	record, err := s.ensureAgentRecordForNode(ctx, node)
	if err != nil {
		return nil, err
	}
	trafficSecret := strings.TrimSpace(node.TrafficStatsSecret)
	if trafficSecret == "" {
		trafficSecret = node.ObfsPassword
	}
	return map[string]any{
		"agent_id":                   record["agent_id"],
		"agent_secret":               record["shared_secret"],
		"report_interval_seconds":    record["report_interval_seconds"],
		"task_poll_interval_seconds": record["task_poll_interval_seconds"],
		"panel_api_base_url":         strings.TrimRight(panelAPIBaseURL, "/"),
		"service_name":               defaultString(node.ServiceName, "hysteria-server"),
		"config_path":                defaultString(node.ConfigPath, "/etc/hysteria/config.yaml"),
		"traffic_stats_listen":       defaultString(node.TrafficStatsListen, "127.0.0.1:9999"),
		"traffic_stats_secret":       trafficSecret,
		"local_auth_listen":          "127.0.0.1:18081",
		"local_auth_state_path":      "/var/lib/mxinhy-agent/local-auth.json",
		"agent_install_path":         defaultString(node.AgentInstallPath, "/usr/local/bin/mxinhy-agent"),
		"agent_config_path":          defaultString(node.AgentConfigPath, "/etc/mxinhy-agent.json"),
		"agent_service_name":         defaultString(node.AgentServiceName, "mxinhy-agent"),
	}, nil
}

func (s *agentService) authenticateRequest(ctx context.Context, request *http.Request, rawBody string) (map[string]any, error) {
	agentID := strings.TrimSpace(request.Header.Get("X-Agent-Id"))
	timestamp := strings.TrimSpace(request.Header.Get("X-Agent-Timestamp"))
	nonce := strings.TrimSpace(request.Header.Get("X-Agent-Nonce"))
	signature := strings.TrimSpace(request.Header.Get("X-Agent-Signature"))
	if agentID == "" || timestamp == "" || nonce == "" || signature == "" {
		return nil, errors.New("Agent 鉴权头缺失")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || ts < 1 || absInt64(time.Now().Unix()-ts) > agentRequestToleranceSeconds {
		return nil, errors.New("Agent 请求已过期")
	}
	record, err := s.getAgentRecordByAgentID(ctx, agentID)
	if err != nil || record == nil {
		return nil, errors.New("Agent 不存在")
	}
	expected := s.signAgentRequest(toString(record["shared_secret"]), timestamp, nonce, rawBody)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, errors.New("Agent 鉴权失败")
	}
	return record, nil
}

func (s *agentService) signAgentRequest(secret, timestamp, nonce, rawBody string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(rawBody))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *agentService) buildDownloadSignature(target, agentID string, expiresAt int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(target + "." + agentID + "." + strconv.FormatInt(expiresAt, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *agentService) authorizeDownload(ctx context.Context, target, agentID string, expiresAt int64, signature string) error {
	if target != "linux-amd64" && target != "linux-arm64" {
		return errors.New("Agent 下载目标无效")
	}
	if agentID == "" || expiresAt < 1 || signature == "" {
		return errors.New("Agent 下载凭证无效")
	}
	now := time.Now().Unix()
	if expiresAt < now || expiresAt > now+agentDownloadTokenMaxTTL {
		return errors.New("Agent 下载凭证已过期")
	}
	record, err := s.getAgentRecordByAgentID(ctx, agentID)
	if err != nil || record == nil {
		return errors.New("Agent 不存在")
	}
	expected := s.buildDownloadSignature(target, agentID, expiresAt, toString(record["shared_secret"]))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return errors.New("Agent 下载凭证无效")
	}
	return nil
}

func (s *agentService) register(ctx context.Context, record map[string]any, payload map[string]any, ip string) (map[string]any, error) {
	capabilities, _ := json.Marshal(payload["capabilities"])
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_agents
		SET status=?, version=?, capabilities_json=?, installed_at=COALESCE(installed_at, NOW()), last_seen_at=NOW(),
		    last_ip=?, last_error=NULL, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		agentStatusOnline, defaultString(toTrimmedString(payload["version"]), "0.1.0"), string(capabilities), ip, record["id"])
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (s *agentService) heartbeat(ctx context.Context, record map[string]any, payload map[string]any, ip string) (map[string]any, error) {
	serviceMessage := decodeBase64Field(toString(payload["service_message_base64"]))
	lastPayload, _ := json.Marshal(payload)
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_agents
		SET status=?, version=?, last_seen_at=NOW(), last_ip=?, last_error=NULL, last_service_status=?, last_service_message=?,
		    last_total_rx=?, last_total_tx=?, last_user_count=?, last_online_count=?, last_payload_json=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		agentStatusOnline, defaultString(toTrimmedString(payload["version"]), defaultString(toString(record["version"]), "0.1.0")),
		ip, defaultString(toTrimmedString(payload["service_status"]), "unknown"), serviceMessage,
		maxInt64(int64Value(payload["total_rx"]), 0), maxInt64(int64Value(payload["total_tx"]), 0),
		maxInt(intValue(payload["user_count"]), 0), maxInt(intValue(payload["online_count"]), 0), string(lastPayload), record["id"])
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (s *agentService) enqueueTask(ctx context.Context, nodeID int64, opcode int, payload map[string]any, createdBy int64, expiresInSeconds int) (map[string]any, error) {
	agent, err := s.getAgentRecordByNodeID(ctx, nodeID)
	if err != nil || agent == nil {
		return nil, errors.New("节点 Agent 尚未注册")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, _ := json.Marshal(payload)
	if len(payload) == 0 {
		payloadJSON = []byte(`{}`)
	}
	if expiresInSeconds < 10 {
		expiresInSeconds = 120
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_tasks (node_id, agent_id, opcode, payload_json, status, created_by, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeID, agent["agent_id"], opcode, string(payloadJSON), taskStatusPending, nullIfZero(createdBy), time.Now().Add(time.Duration(expiresInSeconds)*time.Second).Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	taskID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.getTask(ctx, taskID)
}

func (s *agentService) pullTask(ctx context.Context, record map[string]any) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, agent_id, opcode, payload_json, status, created_by, exit_code, output_text, result_json, error_message, expires_at, started_at, finished_at, created_at, updated_at
		FROM agent_tasks
		WHERE node_id = ? AND agent_id = ? AND status = ? AND (expires_at IS NULL OR expires_at >= NOW())
		ORDER BY id ASC LIMIT 1`, record["node_id"], record["agent_id"], taskStatusPending)
	task, err := s.scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE agent_tasks SET status=?, started_at=NOW(), updated_at=CURRENT_TIMESTAMP WHERE id=? AND status=?`, taskStatusRunning, task["id"], taskStatusPending)
	if err != nil {
		return nil, err
	}
	task, err = s.getTask(ctx, int64Value(task["id"]))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":           task["id"],
		"opcode":       task["opcode"],
		"opcode_label": opcodeLabel(intValue(task["opcode"])),
		"payload":      task["payload_json"],
	}, nil
}

func (s *agentService) completeTask(ctx context.Context, record map[string]any, taskID int64, payload map[string]any) (map[string]any, error) {
	task, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if int64Value(task["node_id"]) != int64Value(record["node_id"]) {
		return nil, errors.New("任务不属于当前 Agent")
	}

	status := taskStatusDone
	if toTrimmedString(payload["status"]) == "error" {
		status = taskStatusError
	}
	output := decodeBase64Field(toString(payload["output_base64"]))
	errorMessage := decodeBase64Field(toString(payload["error_message_base64"]))
	resultPayload := decodeJSONBase64Field(toString(payload["result_base64"]))
	resultJSON, _ := json.Marshal(resultPayload)
	exitCode := intValue(payload["exit_code"])
	if status == taskStatusDone && exitCode == 0 {
		exitCode = 0
	} else if exitCode == 0 {
		exitCode = 1
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status=?, exit_code=?, output_text=?, result_json=?, error_message=?, finished_at=NOW(), updated_at=CURRENT_TIMESTAMP
		WHERE id=?`, status, exitCode, output, string(resultJSON), nullIfEmpty(errorMessage), taskID)
	if err != nil {
		return nil, err
	}
	if status == taskStatusError {
		_, _ = s.db.ExecContext(ctx, `UPDATE node_agents SET status=?, last_error=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, agentStatusError, defaultString(errorMessage, "Agent 任务执行失败"), record["id"])
	}
	return map[string]any{"ok": true}, nil
}

func (s *agentService) waitForTask(ctx context.Context, taskID int64, timeoutSeconds int) (map[string]any, error) {
	if timeoutSeconds < 1 {
		timeoutSeconds = 20
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.getTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		status := toString(task["status"])
		if status == taskStatusDone || status == taskStatusError {
			return task, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, errors.New("等待 Agent 响应超时，请稍后刷新节点状态后重试")
}

func (s *agentService) getTask(ctx context.Context, taskID int64) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, agent_id, opcode, payload_json, status, created_by, exit_code, output_text, result_json,
		       error_message, expires_at, started_at, finished_at, created_at, updated_at
		FROM agent_tasks WHERE id = ? LIMIT 1`, taskID)
	return s.scanTask(row)
}

func (s *agentService) buildTaskResult(task map[string]any, command string) map[string]any {
	exitCode := intValue(task["exit_code"])
	status := toString(task["status"])
	if status == taskStatusDone && exitCode == 0 {
		exitCode = 0
	} else if exitCode == 0 {
		exitCode = 1
	}
	return map[string]any{
		"command":    command,
		"output":     defaultString(toString(task["output_text"]), toString(task["error_message"])),
		"exitCode":   exitCode,
		"payload":    mapValue(task["result_json"]),
		"taskStatus": status,
	}
}

func (s *agentService) markAgentOfflineByNodeID(ctx context.Context, nodeID int64, reason string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE node_agents SET status=?, last_error=?, updated_at=CURRENT_TIMESTAMP WHERE node_id=?`, agentStatusOffline, nullIfEmpty(reason), nodeID)
}

func opcodeLabel(opcode int) string {
	switch opcode {
	case opcodeRestartHY2:
		return "重启 hy2"
	case opcodeStopHY2:
		return "停止 hy2"
	case opcodeStartHY2:
		return "启动 hy2"
	case opcodeStatusHY2:
		return "查询 hy2 状态"
	case opcodeFetchLogs:
		return "获取节点日志"
	case opcodeFetchOnline:
		return "获取在线连接"
	case opcodeFetchStreams:
		return "获取连接详情"
	case opcodeFetchUserTraffic:
		return "获取用户流量"
	case opcodeSyncTrafficClear:
		return "同步并清空流量"
	case opcodeWriteConfig:
		return "下发并应用配置"
	case opcodeRestartAgent:
		return "重启 Agent"
	case opcodeKickClients:
		return "踢下线用户"
	case opcodeSyncLocalAuth:
		return "同步本地鉴权"
	default:
		return "未知任务"
	}
}

func (s *agentService) scanAgentRecord(scanner interface{ Scan(...any) error }) (map[string]any, error) {
	var (
		id                      int64
		nodeID                  int64
		agentID                 string
		sharedSecret            string
		status                  string
		version                 string
		capabilitiesJSON        sql.NullString
		reportIntervalSeconds   int
		taskPollIntervalSeconds int
		installedAt             sql.NullTime
		lastSeenAt              sql.NullTime
		lastIP                  sql.NullString
		lastError               sql.NullString
		lastServiceStatus       sql.NullString
		lastServiceMessage      sql.NullString
		lastTotalRX             int64
		lastTotalTX             int64
		lastUserCount           int64
		lastOnlineCount         int64
		lastPayloadJSON         sql.NullString
		createdAt               time.Time
		updatedAt               time.Time
	)
	err := scanner.Scan(&id, &nodeID, &agentID, &sharedSecret, &status, &version, &capabilitiesJSON, &reportIntervalSeconds, &taskPollIntervalSeconds, &installedAt, &lastSeenAt, &lastIP, &lastError, &lastServiceStatus, &lastServiceMessage, &lastTotalRX, &lastTotalTX, &lastUserCount, &lastOnlineCount, &lastPayloadJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	decryptedSecret, err := DecryptValue(sharedSecret, *s.cfg)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":                         id,
		"node_id":                    nodeID,
		"agent_id":                   agentID,
		"shared_secret":              decryptedSecret,
		"status":                     status,
		"version":                    version,
		"capabilities_json":          decodeJSONColumn(capabilitiesJSON),
		"report_interval_seconds":    reportIntervalSeconds,
		"task_poll_interval_seconds": taskPollIntervalSeconds,
		"installed_at":               nullTimeValue(installedAt),
		"last_seen_at":               nullTimeValue(lastSeenAt),
		"last_ip":                    nullStringValue(lastIP),
		"last_error":                 nullStringValue(lastError),
		"last_service_status":        nullStringValue(lastServiceStatus),
		"last_service_message":       nullStringValue(lastServiceMessage),
		"last_total_rx":              lastTotalRX,
		"last_total_tx":              lastTotalTX,
		"last_user_count":            lastUserCount,
		"last_online_count":          lastOnlineCount,
		"last_payload_json":          decodeJSONColumn(lastPayloadJSON),
		"created_at":                 formatTime(createdAt),
		"updated_at":                 formatTime(updatedAt),
	}, nil
}

func (s *agentService) scanTask(scanner interface{ Scan(...any) error }) (map[string]any, error) {
	var (
		id           int64
		nodeID       int64
		agentID      string
		opcode       int
		payloadJSON  sql.NullString
		status       string
		createdBy    sql.NullInt64
		exitCode     sql.NullInt64
		outputText   sql.NullString
		resultJSON   sql.NullString
		errorMessage sql.NullString
		expiresAt    sql.NullTime
		startedAt    sql.NullTime
		finishedAt   sql.NullTime
		createdAt    time.Time
		updatedAt    time.Time
	)
	err := scanner.Scan(&id, &nodeID, &agentID, &opcode, &payloadJSON, &status, &createdBy, &exitCode, &outputText, &resultJSON, &errorMessage, &expiresAt, &startedAt, &finishedAt, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":            id,
		"node_id":       nodeID,
		"agent_id":      agentID,
		"opcode":        opcode,
		"payload_json":  decodeJSONColumn(payloadJSON),
		"status":        status,
		"created_by":    createdBy.Int64,
		"exit_code":     exitCode.Int64,
		"output_text":   nullStringValue(outputText),
		"result_json":   decodeJSONColumn(resultJSON),
		"error_message": nullStringValue(errorMessage),
		"expires_at":    nullTimeValue(expiresAt),
		"started_at":    nullTimeValue(startedAt),
		"finished_at":   nullTimeValue(finishedAt),
		"created_at":    formatTime(createdAt),
		"updated_at":    formatTime(updatedAt),
	}, nil
}

func decodeBase64Field(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func decodeJSONBase64Field(value string) map[string]any {
	text := decodeBase64Field(value)
	if strings.TrimSpace(text) == "" {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return map[string]any{}
	}
	return data
}

func decodeJSONColumn(value any) map[string]any {
	switch typed := value.(type) {
	case sql.NullString:
		if !typed.Valid || strings.TrimSpace(typed.String) == "" {
			return map[string]any{}
		}
		return decodeJSONString(typed.String)
	case string:
		return decodeJSONString(typed)
	default:
		return map[string]any{}
	}
}

func decodeJSONString(value string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return map[string]any{}
	}
	return data
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullIfZero(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func mapValue(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func maxInt64(value int64, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
