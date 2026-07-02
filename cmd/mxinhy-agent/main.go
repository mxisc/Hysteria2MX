package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const agentVersion = "0.1.0"
const logDir = "/var/log/mxinhy-agent"
const agentLogPath = logDir + "/agent.log"
const hysteriaLogPath = logDir + "/hysteria.log"
const heartbeatFailureGraceMultiplier = 3
const defaultLocalAuthListen = "127.0.0.1:18081"
const defaultLocalAuthStatePath = "/var/lib/mxinhy-agent/local-auth.json"

var errNodeDeleted = errors.New("node deleted by panel")

type config struct {
	NodeID                  int    `json:"node_id"`
	AgentID                 string `json:"agent_id"`
	AgentSecret             string `json:"agent_secret"`
	PanelAPIBaseURL         string `json:"panel_api_base_url"`
	ServiceName             string `json:"service_name"`
	ConfigPath              string `json:"config_path"`
	TrafficStatsListen      string `json:"traffic_stats_listen"`
	TrafficStatsSecret      string `json:"traffic_stats_secret"`
	LocalAuthListen         string `json:"local_auth_listen"`
	LocalAuthStatePath      string `json:"local_auth_state_path"`
	ReportIntervalSeconds   int    `json:"report_interval_seconds"`
	TaskPollIntervalSeconds int    `json:"task_poll_interval_seconds"`
	AgentServiceName        string `json:"agent_service_name"`
	AgentConfigPath         string `json:"-"`
	AgentInstallPath        string `json:"-"`
}

type localAuthUser struct {
	Username     string `json:"username"`
	Credential   string `json:"credential"`
	Status       string `json:"status"`
	QuotaGB      int64  `json:"quota_gb"`
	UsedGB       int64  `json:"used_gb"`
	ExpiresAt    string `json:"expires_at"`
	Subscribable bool   `json:"subscribable"`
}

type localAuthState struct {
	Users []localAuthUser `json:"users"`
}

type apiEnvelope[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type registerPayload struct {
	NodeID       int      `json:"node_id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type heartbeatPayload struct {
	NodeID               int                       `json:"node_id"`
	Version              string                    `json:"version"`
	ServiceStatus        string                    `json:"service_status"`
	ServiceMessageBase64 string                    `json:"service_message_base64"`
	TotalRX              uint64                    `json:"total_rx"`
	TotalTX              uint64                    `json:"total_tx"`
	UserCount            int                       `json:"user_count"`
	OnlineCount          int                       `json:"online_count"`
	UserTraffic          map[string]trafficCounter `json:"user_traffic"`
}

type pullTaskResponse struct {
	Task *task `json:"task"`
}

type task struct {
	ID      int                    `json:"id"`
	Opcode  int                    `json:"opcode"`
	Payload map[string]interface{} `json:"payload"`
}

func (t *task) UnmarshalJSON(data []byte) error {
	type rawTask struct {
		ID      int             `json:"id"`
		Opcode  int             `json:"opcode"`
		Payload json.RawMessage `json:"payload"`
	}

	var raw rawTask
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	t.ID = raw.ID
	t.Opcode = raw.Opcode
	t.Payload = map[string]interface{}{}

	trimmed := strings.TrimSpace(string(raw.Payload))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return nil
	}

	return json.Unmarshal(raw.Payload, &t.Payload)
}

type taskCompletePayload struct {
	Status             string `json:"status"`
	ExitCode           int    `json:"exit_code"`
	OutputBase64       string `json:"output_base64"`
	ResultBase64       string `json:"result_base64"`
	ErrorMessageBase64 string `json:"error_message_base64"`
}

type agent struct {
	cfg    config
	client *http.Client

	mu                   sync.Mutex
	registered           bool
	lastHeartbeatSuccess time.Time
	heartbeatGuardActive bool
	selfUninstalling     bool

	authMu                 sync.RWMutex
	localAuthUsers         map[string]localAuthUser
	localAuthListenerAlive bool
}

type heartbeatSummary struct {
	ServiceStatus  string
	ServiceMessage string
	TotalRX        uint64
	TotalTX        uint64
	UserCount      int
	OnlineCount    int
	UserTraffic    map[string]trafficCounter
}

type trafficCounter struct {
	RX int64 `json:"rx"`
	TX int64 `json:"tx"`
}

type commandResult struct {
	ExitCode int
	Output   string
	Data     interface{}
}

func main() {
	configPath := flag.String("config", "/etc/mxinhy-agent.json", "agent config path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	if err := setupFileLogging(); err != nil {
		fatal(err)
	}

	a := &agent{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		localAuthUsers: map[string]localAuthUser{},
	}

	if err := a.loadLocalAuthState(); err != nil {
		fatal(err)
	}
	if err := a.startLocalAuthServer(); err != nil {
		fatal(err)
	}
	if err := a.run(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	writeLog("error", "mxinhy-agent: %v", err)
	os.Exit(1)
}

func writeLog(level string, format string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, "[%s][%s] %s\n", timestamp, strings.ToLower(strings.TrimSpace(level)), message)
}

func setupFileLogging() error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(agentLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	os.Stdout = file
	os.Stderr = file
	return nil
}

func loadConfig(path string) (config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}

	var cfg config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return config{}, err
	}

	if cfg.NodeID < 1 || cfg.AgentID == "" || cfg.AgentSecret == "" || cfg.PanelAPIBaseURL == "" {
		return config{}, errors.New("agent config is incomplete")
	}
	if cfg.ReportIntervalSeconds < 1 {
		cfg.ReportIntervalSeconds = 2
	}
	if cfg.TaskPollIntervalSeconds < 1 {
		cfg.TaskPollIntervalSeconds = 1
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "hysteria-server"
	}
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = "/etc/hysteria/config.yaml"
	}
	if cfg.AgentServiceName == "" {
		cfg.AgentServiceName = "mxinhy-agent"
	}
	if cfg.LocalAuthListen == "" {
		cfg.LocalAuthListen = defaultLocalAuthListen
	}
	if cfg.LocalAuthStatePath == "" {
		cfg.LocalAuthStatePath = defaultLocalAuthStatePath
	}
	cfg.AgentConfigPath = path
	if executablePath, err := os.Executable(); err == nil {
		cfg.AgentInstallPath = executablePath
	}

	return cfg, nil
}

func (a *agent) run() error {
	a.setLastHeartbeatSuccess(time.Now())
	if err := a.register(); err != nil {
		if errors.Is(err, errNodeDeleted) {
			a.scheduleSelfUninstall("panel marked node deleted")
			return nil
		}
		writeLog("warn", "initial register failed: %v", err)
	} else {
		a.markRegistered()
		writeLog("info", "agent registered: node_id=%d service=%s", a.cfg.NodeID, a.cfg.ServiceName)
	}
	if err := a.sendHeartbeat(); err != nil {
		if errors.Is(err, errNodeDeleted) {
			a.scheduleSelfUninstall("panel marked node deleted")
			return nil
		}
		writeLog("warn", "initial heartbeat failed: %v", err)
		a.handleHeartbeatFailure()
	} else {
		a.noteHeartbeatSuccess()
		writeLog("info", "initial heartbeat sent")
	}

	heartbeatTicker := time.NewTicker(time.Duration(a.cfg.ReportIntervalSeconds) * time.Second)
	defer heartbeatTicker.Stop()
	taskTicker := time.NewTicker(time.Duration(a.cfg.TaskPollIntervalSeconds) * time.Second)
	defer taskTicker.Stop()

	for {
		select {
		case <-heartbeatTicker.C:
			if !a.isRegistered() {
				if err := a.register(); err != nil {
					if errors.Is(err, errNodeDeleted) {
						a.scheduleSelfUninstall("panel marked node deleted")
						return nil
					}
					writeLog("warn", "register retry failed: %v", err)
				} else {
					a.markRegistered()
					writeLog("info", "agent register retry succeeded")
				}
			}
			if err := a.sendHeartbeat(); err != nil {
				if errors.Is(err, errNodeDeleted) {
					a.scheduleSelfUninstall("panel marked node deleted")
					return nil
				}
				writeLog("warn", "heartbeat failed: %v", err)
				a.handleHeartbeatFailure()
			} else {
				a.noteHeartbeatSuccess()
			}
		case <-taskTicker.C:
			if err := a.processNextTask(); err != nil {
				if errors.Is(err, errNodeDeleted) {
					a.scheduleSelfUninstall("panel marked node deleted")
					return nil
				}
				writeLog("error", "task processing failed: %v", err)
			}
		}
	}
}

func (a *agent) isRegistered() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.registered
}

func (a *agent) markRegistered() {
	a.mu.Lock()
	a.registered = true
	a.mu.Unlock()
}

func (a *agent) setLastHeartbeatSuccess(value time.Time) {
	a.mu.Lock()
	a.lastHeartbeatSuccess = value
	a.mu.Unlock()
}

func (a *agent) heartbeatDisableAfter() time.Duration {
	reportInterval := a.cfg.ReportIntervalSeconds
	if reportInterval < 1 {
		reportInterval = 2
	}
	return time.Duration(reportInterval*heartbeatFailureGraceMultiplier) * time.Second
}

func (a *agent) noteHeartbeatSuccess() {
	var shouldLog bool

	a.mu.Lock()
	a.lastHeartbeatSuccess = time.Now()
	shouldLog = a.heartbeatGuardActive
	a.heartbeatGuardActive = false
	a.mu.Unlock()

	if !shouldLog {
		return
	}
	writeLog("info", "heartbeat guard cleared; new connections are accepted again")
}

func (a *agent) handleHeartbeatFailure() {
	var shouldEngage bool
	now := time.Now()

	a.mu.Lock()
	if a.lastHeartbeatSuccess.IsZero() {
		a.lastHeartbeatSuccess = now
	}
	if !a.heartbeatGuardActive && now.Sub(a.lastHeartbeatSuccess) >= a.heartbeatDisableAfter() {
		a.heartbeatGuardActive = true
		shouldEngage = true
	}
	a.mu.Unlock()

	if !shouldEngage {
		return
	}
	writeLog("warn", "heartbeat guard engaged: new connections are rejected after %s without successful heartbeats", a.heartbeatDisableAfter().Round(time.Second))
}

func (a *agent) scheduleSelfUninstall(reason string) {
	a.mu.Lock()
	if a.selfUninstalling {
		a.mu.Unlock()
		return
	}
	a.selfUninstalling = true
	a.heartbeatGuardActive = true
	a.mu.Unlock()

	a.setLocalAuthState(localAuthState{Users: []localAuthUser{}})
	_ = os.Remove(a.cfg.LocalAuthStatePath)
	writeLog("warn", "self uninstall scheduled: %s", reason)

	command := fmt.Sprintf(
		`mkdir -p /var/log/mxinhy-agent; (sleep 1; systemctl stop %s 2>/dev/null || true; systemctl disable %s 2>/dev/null || true; rm -f %s; rm -rf %s; rm -f %s; systemctl daemon-reload 2>/dev/null || true; systemctl stop %s 2>/dev/null || true; systemctl disable %s 2>/dev/null || true; rm -f %s; rm -rf %s; rm -f %s; rm -f %s; systemctl daemon-reload 2>/dev/null || true) >/var/log/mxinhy-agent/self-uninstall.log 2>&1 &`,
		shellQuote(a.cfg.ServiceName),
		shellQuote(a.cfg.ServiceName),
		shellQuote(a.cfg.ConfigPath),
		shellQuote("/etc/systemd/system/"+a.cfg.ServiceName+".service.d"),
		shellQuote(a.cfg.LocalAuthStatePath),
		shellQuote(a.cfg.AgentServiceName),
		shellQuote(a.cfg.AgentServiceName),
		shellQuote("/etc/systemd/system/"+a.cfg.AgentServiceName+".service"),
		shellQuote("/etc/systemd/system/"+a.cfg.AgentServiceName+".service.d"),
		shellQuote(a.cfg.AgentConfigPath),
		shellQuote(a.cfg.AgentInstallPath),
	)
	_, _ = runCommand(command)
}

func (a *agent) register() error {
	payload := registerPayload{
		NodeID:       a.cfg.NodeID,
		Version:      agentVersion,
		Capabilities: []string{"service", "logs", "traffic", "streams", "config", "kick_clients", "local_auth"},
	}

	return a.postJSON("/agent/register", payload, nil)
}

func (a *agent) sendHeartbeat() error {
	summary := a.collectHeartbeatSummary()
	payload := heartbeatPayload{
		NodeID:               a.cfg.NodeID,
		Version:              agentVersion,
		ServiceStatus:        summary.ServiceStatus,
		ServiceMessageBase64: base64.StdEncoding.EncodeToString([]byte(summary.ServiceMessage)),
		TotalRX:              summary.TotalRX,
		TotalTX:              summary.TotalTX,
		UserCount:            summary.UserCount,
		OnlineCount:          summary.OnlineCount,
		UserTraffic:          summary.UserTraffic,
	}

	return a.postJSON("/agent/heartbeat", payload, nil)
}

func (a *agent) processNextTask() error {
	var response pullTaskResponse
	if err := a.postJSON("/agent/tasks/pull", map[string]int{"node_id": a.cfg.NodeID}, &response); err != nil {
		return err
	}

	if response.Task == nil || response.Task.ID < 1 {
		return nil
	}

	result := a.executeTask(*response.Task)
	status := "done"
	errorMessage := ""
	if result.ExitCode != 0 {
		status = "error"
		errorMessage = result.Output
	}

	resultJSON, _ := json.Marshal(result.Data)
	completePayload := taskCompletePayload{
		Status:             status,
		ExitCode:           result.ExitCode,
		OutputBase64:       base64.StdEncoding.EncodeToString([]byte(result.Output)),
		ResultBase64:       base64.StdEncoding.EncodeToString(resultJSON),
		ErrorMessageBase64: base64.StdEncoding.EncodeToString([]byte(errorMessage)),
	}

	if err := a.postJSON(fmt.Sprintf("/agent/tasks/%d/complete", response.Task.ID), completePayload, nil); err != nil {
		return err
	}

	if response.Task.Opcode == 11 && result.ExitCode == 0 {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			_, _ = runCommand("systemctl restart " + shellQuote(a.cfg.AgentServiceName))
		}()
	}

	return nil
}

func (a *agent) executeTask(task task) commandResult {
	switch task.Opcode {
	case 1:
		return a.serviceAction("restart", true)
	case 2:
		return a.serviceAction("stop", false)
	case 3:
		return a.serviceAction("start", false)
	case 4:
		return a.serviceAction("status", false)
	case 5:
		lines := intFromPayload(task.Payload, "lines", 200)
		return a.fetchLogs(lines)
	case 6:
		return a.fetchOnline()
	case 7:
		return a.fetchStreams()
	case 8:
		return a.fetchUserTraffic(task.Payload)
	case 9:
		return a.syncTrafficUsage(boolFromPayload(task.Payload, "clear", true))
	case 10:
		return a.writeConfig(task.Payload)
	case 11:
		return commandResult{ExitCode: 0, Output: "agent restart scheduled", Data: map[string]string{"message": "agent restart scheduled"}}
	case 12:
		return a.kickClients(task.Payload)
	case 13:
		return a.syncLocalAuth(task.Payload)
	default:
		return commandResult{ExitCode: 1, Output: "unsupported opcode", Data: map[string]string{"error": "unsupported opcode"}}
	}
}

func (a *agent) collectHeartbeatSummary() heartbeatSummary {
	serviceStatus, serviceMessage := a.currentServiceStatus()
	trafficData, err := a.fetchTrafficPayload(false)
	if err != nil {
		return heartbeatSummary{
			ServiceStatus:  serviceStatus,
			ServiceMessage: serviceMessage,
		}
	}

	var totalRX uint64
	var totalTX uint64
	userTraffic := make(map[string]trafficCounter, len(trafficData))
	userCount := 0
	for username, item := range trafficData {
		if stats, ok := item.(map[string]interface{}); ok {
			rx := numberFromAny(stats["rx"])
			tx := numberFromAny(stats["tx"])
			totalRX += uint64(rx)
			totalTX += uint64(tx)
			userTraffic[username] = trafficCounter{RX: rx, TX: tx}
			userCount++
		}
	}

	onlinePayload, err := a.fetchOnlinePayload()
	if err != nil {
		return heartbeatSummary{
			ServiceStatus:  serviceStatus,
			ServiceMessage: serviceMessage,
			TotalRX:        totalRX,
			TotalTX:        totalTX,
			UserCount:      userCount,
			OnlineCount:    0,
			UserTraffic:    userTraffic,
		}
	}

	return heartbeatSummary{
		ServiceStatus:  serviceStatus,
		ServiceMessage: serviceMessage,
		TotalRX:        totalRX,
		TotalTX:        totalTX,
		UserCount:      userCount,
		OnlineCount:    len(onlinePayload),
		UserTraffic:    userTraffic,
	}
}

func (a *agent) currentServiceStatus() (string, string) {
	statusOutput, err := runCommand("systemctl is-active " + shellQuote(a.cfg.ServiceName))
	if err != nil {
		detail, _ := runCommand("systemctl status " + shellQuote(a.cfg.ServiceName) + " --no-pager --lines=20")
		return "degraded", trimOutput(detail)
	}

	status := strings.TrimSpace(statusOutput)
	guardSuffix := ""
	if a.isHeartbeatGuardActive() {
		guardSuffix = "\n[local-auth-guard] panel heartbeat unavailable; new connections are temporarily rejected"
	}
	switch status {
	case "active":
		detail, _ := runCommand("systemctl status " + shellQuote(a.cfg.ServiceName) + " --no-pager --lines=10")
		return "running", trimOutput(detail) + guardSuffix
	case "inactive", "failed":
		detail, _ := runCommand("systemctl status " + shellQuote(a.cfg.ServiceName) + " --no-pager --lines=10")
		return "stopped", trimOutput(detail) + guardSuffix
	default:
		detail, _ := runCommand("systemctl status " + shellQuote(a.cfg.ServiceName) + " --no-pager --lines=10")
		return "degraded", trimOutput(detail) + guardSuffix
	}
}

func (a *agent) serviceAction(action string, refreshSummary bool) commandResult {
	if err := a.ensureServiceFileLogging(); err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}

	allowed := map[string]bool{
		"start":   true,
		"stop":    true,
		"restart": true,
		"status":  true,
	}
	if !allowed[action] {
		return commandResult{ExitCode: 1, Output: "unsupported service action"}
	}

	command := "systemctl " + action + " " + shellQuote(a.cfg.ServiceName)
	if action == "status" {
		command = "systemctl status " + shellQuote(a.cfg.ServiceName) + " --no-pager --lines=20"
	}
	output, err := runCommand(command)
	exitCode := 0
	if err != nil {
		exitCode = exitCodeFromErr(err)
		output = strings.TrimSpace(output)
		if output == "" {
			output = err.Error()
		}
	}

	data := map[string]interface{}{}
	if refreshSummary {
		serviceStatus, serviceMessage := a.currentServiceStatus()
		data["service_status"] = serviceStatus
		data["service_message"] = serviceMessage
	}

	return commandResult{
		ExitCode: exitCode,
		Output:   trimOutput(output),
		Data:     data,
	}
}

func (a *agent) fetchLogs(lines int) commandResult {
	if err := a.ensureServiceFileLogging(); err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	if lines < 10 {
		lines = 10
	}
	if lines > 500 {
		lines = 500
	}

	output, err := runCommand(fmt.Sprintf("tail -n %d %s", lines, shellQuote(hysteriaLogPath)))
	exitCode := 0
	if err != nil {
		exitCode = exitCodeFromErr(err)
		output = strings.TrimSpace(output)
		if output == "" {
			output = err.Error()
		}
	}

	return commandResult{
		ExitCode: exitCode,
		Output:   trimOutput(output),
		Data: map[string]interface{}{
			"logs": trimOutput(output),
		},
	}
}

func (a *agent) fetchOnline() commandResult {
	payload, err := a.fetchOnlinePayload()
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error(), Data: map[string]interface{}{"items": []map[string]interface{}{}}}
	}

	items := make([]map[string]interface{}, 0, len(payload))
	for clientID, rawConnections := range payload {
		items = append(items, map[string]interface{}{
			"id":          clientID,
			"connections": rawConnections,
		})
	}

	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("fetched %d online clients", len(items)),
		Data: map[string]interface{}{
			"items": items,
		},
	}
}

func (a *agent) fetchStreams() commandResult {
	payload, err := a.fetchStatsJSON("/dump/streams")
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error(), Data: map[string]interface{}{"items": []map[string]interface{}{}}}
	}

	streams, _ := payload["streams"].([]interface{})
	items := make([]interface{}, 0, len(streams))
	for _, item := range streams {
		items = append(items, item)
	}

	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("fetched %d streams", len(items)),
		Data: map[string]interface{}{
			"items": items,
		},
	}
}

func (a *agent) fetchUserTraffic(taskPayload map[string]interface{}) commandResult {
	trafficPayload, err := a.fetchTrafficPayload(false)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error(), Data: map[string]interface{}{"items": []map[string]interface{}{}}}
	}

	var allowed map[string]bool
	if raw, ok := taskPayload["usernames"].([]interface{}); ok && len(raw) > 0 {
		allowed = make(map[string]bool, len(raw))
		for _, item := range raw {
			value := strings.TrimSpace(fmt.Sprintf("%v", item))
			if value != "" {
				allowed[value] = true
			}
		}
	}

	items := make([]map[string]interface{}, 0, len(trafficPayload))
	for username, rawStats := range trafficPayload {
		if allowed != nil && !allowed[username] {
			continue
		}
		stats, ok := rawStats.(map[string]interface{})
		if !ok {
			continue
		}
		rx := numberFromAny(stats["rx"])
		tx := numberFromAny(stats["tx"])
		items = append(items, map[string]interface{}{
			"username":    username,
			"rx":          rx,
			"tx":          tx,
			"rx_human":    humanBytes(rx),
			"tx_human":    humanBytes(tx),
			"total_human": humanBytes(rx + tx),
		})
	}

	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("fetched %d user traffic entries", len(items)),
		Data: map[string]interface{}{
			"items": items,
		},
	}
}

func (a *agent) syncTrafficUsage(clear bool) commandResult {
	payload, err := a.fetchTrafficPayload(clear)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error(), Data: map[string]interface{}{}}
	}

	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("synced %d traffic records", len(payload)),
		Data: map[string]interface{}{
			"traffic": payload,
		},
	}
}

func (a *agent) kickClients(taskPayload map[string]interface{}) commandResult {
	rawClients, ok := taskPayload["clients"].([]interface{})
	if !ok || len(rawClients) == 0 {
		return commandResult{ExitCode: 0, Output: "no clients to kick", Data: map[string]interface{}{"clients": []string{}}}
	}

	clients := make([]string, 0, len(rawClients))
	for _, item := range rawClients {
		value := strings.TrimSpace(fmt.Sprintf("%v", item))
		if value != "" {
			clients = append(clients, value)
		}
	}
	if len(clients) == 0 {
		return commandResult{ExitCode: 0, Output: "no clients to kick", Data: map[string]interface{}{"clients": []string{}}}
	}

	body, err := json.Marshal(clients)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}

	baseURL := normalizeStatsURL(a.cfg.TrafficStatsListen)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/kick", bytes.NewReader(body))
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	req.Header.Set("Authorization", a.cfg.TrafficStatsSecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	if resp.StatusCode >= 400 {
		return commandResult{ExitCode: 1, Output: strings.TrimSpace(string(respBody))}
	}

	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("kicked %d clients", len(clients)),
		Data: map[string]interface{}{
			"clients": clients,
			"result":  strings.TrimSpace(string(respBody)),
		},
	}
}

func (a *agent) writeConfig(taskPayload map[string]interface{}) commandResult {
	if err := a.ensureServiceFileLogging(); err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	if localAuthPayload, ok := taskPayload["local_auth"].(map[string]interface{}); ok {
		if result := a.syncLocalAuth(localAuthPayload); result.ExitCode != 0 {
			return result
		}
	}

	configBase64 := strings.TrimSpace(fmt.Sprintf("%v", taskPayload["config_base64"]))
	if configBase64 == "" {
		return commandResult{ExitCode: 1, Output: "config_base64 is required"}
	}

	decoded, err := base64.StdEncoding.DecodeString(configBase64)
	if err != nil {
		return commandResult{ExitCode: 1, Output: "invalid config_base64"}
	}

	if err := os.MkdirAll(filepath.Dir(a.cfg.ConfigPath), 0o755); err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	existing, err := os.ReadFile(a.cfg.ConfigPath)
	configChanged := err != nil || !bytes.Equal(existing, decoded)
	if configChanged {
		if err := os.WriteFile(a.cfg.ConfigPath, decoded, 0o600); err != nil {
			return commandResult{ExitCode: 1, Output: err.Error()}
		}
	}

	chownCmd := fmt.Sprintf("chown hysteria:hysteria %s 2>/dev/null || true", shellQuote(a.cfg.ConfigPath))
	_, _ = runCommand(chownCmd)
	if !configChanged {
		return commandResult{
			ExitCode: 0,
			Output:   "config unchanged; local auth state updated",
			Data: map[string]interface{}{
				"config_path": a.cfg.ConfigPath,
			},
		}
	}

	restartResult := a.serviceAction("restart", true)
	if restartResult.ExitCode != 0 {
		return restartResult
	}

	return commandResult{
		ExitCode: 0,
		Output:   "config written and service restarted",
		Data: map[string]interface{}{
			"config_path": a.cfg.ConfigPath,
		},
	}
}

func (a *agent) ensureServiceFileLogging() error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	dropInDir := filepath.Join("/etc/systemd/system", a.cfg.ServiceName+".service.d")
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		return err
	}

	dropInPath := filepath.Join(dropInDir, "mxinhy-logging.conf")
	content := "[Service]\n" +
		"StandardOutput=append:" + hysteriaLogPath + "\n" +
		"StandardError=append:" + hysteriaLogPath + "\n"
	existing, _ := os.ReadFile(dropInPath)
	if string(existing) == content {
		return nil
	}

	if err := os.WriteFile(dropInPath, []byte(content), 0o644); err != nil {
		return err
	}

	_, err := runCommand("systemctl daemon-reload")
	return err
}

func (a *agent) syncLocalAuth(taskPayload map[string]interface{}) commandResult {
	state, err := parseLocalAuthState(taskPayload)
	if err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	if err := a.storeLocalAuthState(state); err != nil {
		return commandResult{ExitCode: 1, Output: err.Error()}
	}
	return commandResult{
		ExitCode: 0,
		Output:   fmt.Sprintf("synced %d local auth users", len(state.Users)),
		Data: map[string]interface{}{
			"user_count": len(state.Users),
		},
	}
}

func (a *agent) isHeartbeatGuardActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.heartbeatGuardActive
}

func (a *agent) loadLocalAuthState() error {
	content, err := os.ReadFile(a.cfg.LocalAuthStatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.setLocalAuthState(localAuthState{Users: []localAuthUser{}})
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		a.setLocalAuthState(localAuthState{Users: []localAuthUser{}})
		return nil
	}
	var state localAuthState
	if err := json.Unmarshal(content, &state); err != nil {
		return err
	}
	a.setLocalAuthState(state)
	return nil
}

func (a *agent) storeLocalAuthState(state localAuthState) error {
	if err := os.MkdirAll(filepath.Dir(a.cfg.LocalAuthStatePath), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.cfg.LocalAuthStatePath, body, 0o600); err != nil {
		return err
	}
	a.setLocalAuthState(state)
	return nil
}

func (a *agent) setLocalAuthState(state localAuthState) {
	users := make(map[string]localAuthUser, len(state.Users))
	for _, user := range state.Users {
		credential := strings.TrimSpace(user.Credential)
		if credential == "" {
			continue
		}
		users[credential] = user
	}
	a.authMu.Lock()
	a.localAuthUsers = users
	a.authMu.Unlock()
}

func (a *agent) startLocalAuthServer() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", a.handleLocalAuth)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
	})
	listener, err := net.Listen("tcp", a.cfg.LocalAuthListen)
	if err != nil {
		return err
	}
	a.authMu.Lock()
	a.localAuthListenerAlive = true
	a.authMu.Unlock()
	go func() {
		server := &http.Server{Handler: mux}
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			writeLog("error", "local auth server stopped: %v", err)
		}
	}()
	writeLog("info", "local auth server listening on %s", a.cfg.LocalAuthListen)
	return nil
}

func (a *agent) handleLocalAuth(writer http.ResponseWriter, request *http.Request) {
	payload := map[string]any{}
	rawBody, _ := io.ReadAll(request.Body)
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if len(rawBody) > 0 {
		if strings.Contains(contentType, "application/json") {
			_ = json.Unmarshal(rawBody, &payload)
		} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
			_ = request.ParseForm()
			for key := range request.PostForm {
				payload[key] = request.PostForm.Get(key)
			}
		}
	}
	credential := strings.TrimSpace(fmt.Sprintf("%v", payload["auth"]))
	if credential == "" || credential == "<nil>" {
		credential = strings.TrimSpace(request.URL.Query().Get("auth"))
	}
	if credential == "" {
		credential = strings.TrimSpace(request.Header.Get("Hysteria-Auth"))
	}
	if credential == "" {
		credential = strings.TrimSpace(string(rawBody))
	}
	clientID, err := a.authorizeLocalCredential(credential)
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if err != nil {
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "msg": err.Error()})
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "id": clientID})
}

func (a *agent) authorizeLocalCredential(credential string) (string, error) {
	if a.isHeartbeatGuardActive() {
		return "", errors.New("控制面不可用，当前节点已暂停接收新连接")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", errors.New("认证失败")
	}
	a.authMu.RLock()
	user, ok := a.localAuthUsers[credential]
	a.authMu.RUnlock()
	if !ok {
		return "", errors.New("认证失败")
	}
	if strings.TrimSpace(strings.ToLower(user.Status)) != "active" {
		return "", errors.New("账号已停用")
	}
	if expiresAt, ok := parseLocalAuthTime(user.ExpiresAt); ok && !expiresAt.After(time.Now()) {
		return "", errors.New("账号已过期")
	}
	if user.QuotaGB > 0 && user.UsedGB >= user.QuotaGB {
		return "", errors.New("流量已用尽")
	}
	return user.Username, nil
}

func parseLocalAuthState(payload map[string]interface{}) (localAuthState, error) {
	rawUsers, ok := payload["users"].([]interface{})
	if !ok {
		return localAuthState{}, errors.New("local auth users are required")
	}
	state := localAuthState{Users: make([]localAuthUser, 0, len(rawUsers))}
	for _, raw := range rawUsers {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		state.Users = append(state.Users, localAuthUser{
			Username:     strings.TrimSpace(fmt.Sprintf("%v", item["username"])),
			Credential:   strings.TrimSpace(fmt.Sprintf("%v", item["credential"])),
			Status:       strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", item["status"]))),
			QuotaGB:      int64(numberFromAny(item["quota_gb"])),
			UsedGB:       int64(numberFromAny(item["used_gb"])),
			ExpiresAt:    strings.TrimSpace(fmt.Sprintf("%v", item["expires_at"])),
			Subscribable: boolFromPayload(item, "subscribable", true),
		})
	}
	return state, nil
}

func parseLocalAuthTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (a *agent) fetchOnlinePayload() (map[string]int, error) {
	payload, err := a.fetchStatsJSON("/online")
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(payload))
	for key, value := range payload {
		result[key] = int(numberFromAny(value))
	}

	return result, nil
}

func (a *agent) fetchTrafficPayload(clear bool) (map[string]interface{}, error) {
	path := "/traffic"
	if clear {
		path = "/traffic?clear=1"
	}
	return a.fetchStatsJSON(path)
}

func (a *agent) fetchStatsJSON(path string) (map[string]interface{}, error) {
	baseURL := normalizeStatsURL(a.cfg.TrafficStatsListen)
	if baseURL == "" {
		return nil, errors.New("traffic stats listen is empty")
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", a.cfg.TrafficStatsSecret)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("traffic stats request failed: %s", strings.TrimSpace(string(body)))
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return map[string]interface{}{}, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func (a *agent) postJSON(path string, payload interface{}, target interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(a.cfg.PanelAPIBaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	addAgentAuthHeaders(req, a.cfg.AgentID, a.cfg.AgentSecret, body)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusGone {
		return errNodeDeleted
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("panel request failed: %s", strings.TrimSpace(string(respBody)))
	}

	if target == nil {
		return nil
	}

	switch out := target.(type) {
	case *pullTaskResponse:
		var envelope apiEnvelope[pullTaskResponse]
		if err := json.Unmarshal(respBody, &envelope); err != nil {
			return err
		}
		if !envelope.Success {
			return errors.New(envelope.Message)
		}
		*out = envelope.Data
	default:
		return nil
	}

	return nil
}

func addAgentAuthHeaders(req *http.Request, agentID, secret string, body []byte) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomHex(16)
	signature := signRequest(secret, timestamp, nonce, body)
	req.Header.Set("X-Agent-Id", agentID)
	req.Header.Set("X-Agent-Timestamp", timestamp)
	req.Header.Set("X-Agent-Nonce", nonce)
	req.Header.Set("X-Agent-Signature", signature)
}

func signRequest(secret, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UnixNano()
		return fmt.Sprintf("%x", now)
	}
	return hex.EncodeToString(buf)
}

func normalizeStatsURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "http://127.0.0.1:9999"
	}
	if strings.HasPrefix(listen, "http://") || strings.HasPrefix(listen, "https://") {
		return strings.TrimRight(listen, "/")
	}
	if strings.HasPrefix(listen, ":") {
		return "http://127.0.0.1" + listen
	}
	return "http://" + strings.TrimRight(listen, "/")
}

func runCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		if errText != "" {
			if output != "" {
				output += "\n"
			}
			output += errText
		}
		return output, err
	}

	if output == "" {
		output = errText
	}

	return strings.TrimSpace(output), nil
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if waitStatus, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return waitStatus.ExitStatus()
		}
		return exitErr.ExitCode()
	}
	return 1
}

func intFromPayload(payload map[string]interface{}, key string, fallback int) int {
	raw, ok := payload[key]
	if !ok {
		return fallback
	}
	return int(numberFromAny(raw))
}

func boolFromPayload(payload map[string]interface{}, key string, fallback bool) bool {
	raw, ok := payload[key]
	if !ok {
		return fallback
	}

	switch v := raw.(type) {
	case bool:
		return v
	case string:
		value := strings.TrimSpace(strings.ToLower(v))
		if value == "1" || value == "true" || value == "yes" || value == "on" {
			return true
		}
		if value == "0" || value == "false" || value == "no" || value == "off" {
			return false
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	}

	return fallback
}

func numberFromAny(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		var num json.Number = json.Number(v)
		i, _ := num.Int64()
		return i
	default:
		return 0
	}
}

func trimOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8000 {
		return value
	}
	return value[:8000]
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(value)/1024)
	}
	if value < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(value)/1024/1024)
	}
	return fmt.Sprintf("%.2f GB", float64(value)/1024/1024/1024)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

var _ sync.Locker
