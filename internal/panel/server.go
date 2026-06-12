package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	embeddedassets "mxinhy"
)

type apiEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type App struct {
	config          *Config
	logger          *log.Logger
	embeddedStatic  fs.FS
	db              *sql.DB
	auth            *authService
	admins          *adminService
	agents          *agentService
	audit           *auditService
	settings        *systemSettingsStore
	hysteria        *hysteriaService
	jobs            *jobManager
	remote          *remoteExecutor
	sshKeys         *sshKeyUploadService
	systemHealth    *systemHealthService
	loginProtection *loginProtectionService
}

func NewServer(config Config, logger *log.Logger) (*http.Server, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	var db *sql.DB
	var err error
	if config.IsConfigured() {
		db, err = OpenDB(config)
		if err != nil {
			logger.Printf("open mysql failed: %v", err)
		} else if migrateErr := newMigrationService(config).ensureUpToDate(db); migrateErr != nil {
			_ = db.Close()
			return nil, migrateErr
		}
	}

	loginProtection, err := newLoginProtectionService(&config)
	if err != nil {
		return nil, err
	}
	sshKeys := newSSHKeyUploadService(config)
	remote := newRemoteExecutor()
	agents := newAgentService(db, &config)
	audit := newAuditService(db)
	settings := newSystemSettingsStore(db)
	if db != nil {
		if err := settings.ensureDefaults(context.Background(), config); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := settings.applyToConfig(context.Background(), &config); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	normalizeMockPanelCounts(&config)
	jobs := newJobManager(config)

	app := &App{
		config:          &config,
		logger:          logger,
		embeddedStatic:  embeddedassets.Web(),
		db:              db,
		auth:            newAuthService(db, &config),
		admins:          newAdminService(db),
		agents:          agents,
		audit:           audit,
		settings:        settings,
		hysteria:        newHysteriaService(db, &config, sshKeys, remote, agents),
		jobs:            jobs,
		remote:          remote,
		sshKeys:         sshKeys,
		systemHealth:    newSystemHealthService(db, &config),
		loginProtection: loginProtection,
	}

	return &http.Server{
		Addr:              config.BindAddr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", a.handleHealthz)
	mux.HandleFunc("GET /api/app-settings", a.handleAppSettings)
	mux.HandleFunc("GET /api/mock-mode", a.handleMockMode)
	mux.HandleFunc("GET /api/agent/download/", a.handleAgentDownload)
	mux.HandleFunc("POST /api/agent/register", a.handleAgentRegister)
	mux.HandleFunc("POST /api/agent/heartbeat", a.handleAgentHeartbeat)
	mux.HandleFunc("POST /api/agent/tasks/pull", a.handleAgentPullTask)
	mux.HandleFunc("POST /api/agent/tasks/", a.handleAgentCompleteTask)
	mux.HandleFunc("GET /subscription/", a.handleSubscription)
	mux.HandleFunc("GET /api/hysteria/auth/", a.handleHysteriaAuth)
	mux.HandleFunc("POST /api/hysteria/auth/", a.handleHysteriaAuth)
	mux.HandleFunc("GET /api/setup/status", a.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/init", a.handleSetupInit)
	mux.HandleFunc("GET /api/auth/login-challenge", a.handleLoginChallenge)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/auth/me", a.handleCurrentUser)
	mux.HandleFunc("GET /api/system-health", a.handleSystemHealth)
	mux.HandleFunc("GET /api/system-settings", a.handleSystemSettings)
	mux.HandleFunc("PUT /api/system-settings", a.handleUpdateSystemSettings)
	mux.HandleFunc("GET /api/version/check", a.handleVersionCheck)
	mux.HandleFunc("POST /api/version/upgrade", a.handleVersionUpgrade)
	mux.HandleFunc("GET /api/notification-settings", a.handleNotificationSettings)
	mux.HandleFunc("PUT /api/notification-settings", a.handleUpdateNotificationSettings)
	mux.HandleFunc("POST /api/notification-settings/test", a.handleTestNotificationSettings)
	mux.HandleFunc("GET /api/audit-logs", a.handleAuditLogs)
	mux.HandleFunc("GET /api/admins", a.handleAdmins)
	mux.HandleFunc("POST /api/admins", a.handleCreateAdmin)
	mux.HandleFunc("PUT /api/admins/", a.handleUpdateAdmin)
	mux.HandleFunc("DELETE /api/admins/", a.handleDeleteAdmin)
	mux.HandleFunc("GET /api/panel/state", a.handlePanelState)
	mux.HandleFunc("GET /api/nodes/current", a.handleCurrentNode)
	mux.HandleFunc("GET /api/nodes", a.handleNodes)
	mux.HandleFunc("POST /api/nodes", a.handleCreateNode)
	mux.HandleFunc("POST /api/nodes/install", a.handleCreateNodeAndInstall)
	mux.HandleFunc("POST /api/nodes/ssh-key-upload", a.handleUploadNodeSSHKey)
	mux.HandleFunc("PUT /api/nodes/", a.handleUpdateNode)
	mux.HandleFunc("DELETE /api/nodes/", a.handleDeleteNode)
	mux.HandleFunc("POST /api/nodes/", a.handleSelectNode)
	mux.HandleFunc("GET /api/hysteria/online", a.handleOnlineClients)
	mux.HandleFunc("GET /api/hysteria/streams", a.handleStreams)
	mux.HandleFunc("GET /api/hysteria/traffic-stats", a.handleUserTrafficStats)
	mux.HandleFunc("GET /api/hysteria/traffic-history", a.handleTrafficHistory)
	mux.HandleFunc("GET /api/hysteria/logs", a.handleHysteriaLogs)
	mux.HandleFunc("POST /api/hysteria/install", a.handleHysteriaInstall)
	mux.HandleFunc("POST /api/hysteria/uninstall", a.handleHysteriaUninstall)
	mux.HandleFunc("POST /api/hysteria/deploy-config", a.handleDeployConfig)
	mux.HandleFunc("POST /api/hysteria/sync-traffic", a.handleSyncTraffic)
	mux.HandleFunc("GET /api/jobs/", a.handleJob)
	mux.HandleFunc("POST /api/hysteria/service/", a.handleServiceAction)
	mux.HandleFunc("GET /api/users", a.handleUsers)
	mux.HandleFunc("POST /api/users", a.handleCreateUser)
	mux.HandleFunc("PUT /api/users/", a.handleUpdateUser)
	mux.HandleFunc("DELETE /api/users/", a.handleDeleteUser)
	mux.HandleFunc("GET /api/users/", a.handleUserSubscriptionInfo)
	mux.HandleFunc("/api/", a.handleAPI)
	mux.HandleFunc("/", a.handleStatic)
	return a.loggingMiddleware(mux)
}

func (a *App) handleHealthz(writer http.ResponseWriter, request *http.Request) {
	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data: map[string]any{
			"status":     "ok",
			"env":        a.config.Env,
			"configured": a.config.IsConfigured(),
			"database":   a.db != nil,
		},
	})
}

func (a *App) handleAppSettings(writer http.ResponseWriter, request *http.Request) {
	appSettings := a.config.AppSettings()
	appSettings["login_background_url"] = a.settings.loginBackgroundURL(request.Context())
	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data:    appSettings,
	})
}

func (a *App) handleMockMode(writer http.ResponseWriter, request *http.Request) {
	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data: map[string]any{
			"enabled":              a.config.MockPanel,
			"node_count":           a.config.MockNodeCount,
			"user_count":           a.config.MockUserCount,
			"running_node_count":   a.config.MockRunningNodeCount,
			"degraded_node_count":  a.config.MockDegradedNodeCount,
			"stopped_node_count":   a.config.MockStoppedNodeCount,
			"suspended_user_count": a.config.MockSuspendedUserCount,
		},
	})
}

func (a *App) handleSubscription(writer http.ResponseWriter, request *http.Request) {
	id, ok := parseTrailingID(request.URL.Path, "/subscription/")
	if !ok {
		http.NotFound(writer, request)
		return
	}
	content, err := a.hysteria.renderUserSubscription(request.Context(), id, strings.TrimSpace(request.URL.Query().Get("token")))
	if err != nil {
		writeError(writer, http.StatusForbidden, "订阅链接无效或当前没有可用节点")
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store, private")
	_, _ = writer.Write([]byte(content))
}

func (a *App) handleAgentDownload(writer http.ResponseWriter, request *http.Request) {
	target := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/agent/download/"), "/")
	if err := a.agents.authorizeDownload(request.Context(), target, strings.TrimSpace(request.URL.Query().Get("agent_id")), int64(readQueryInt(request, "expires", 0)), strings.TrimSpace(request.URL.Query().Get("signature"))); err != nil {
		writeError(writer, http.StatusForbidden, "Agent 下载链接无效或已过期")
		return
	}
	binaryPath := filepath.Join(projectRootFromConfig(*a.config), "artifacts", "agent", "mxinhy-agent-"+target)
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		writeError(writer, http.StatusNotFound, "Agent 安装包暂不可用，请稍后重试")
		return
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", `attachment; filename="mxinhy-agent-`+target+`"`)
	writer.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	writer.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(writer, request, binaryPath)
}

func (a *App) handleHysteriaAuth(writer http.ResponseWriter, request *http.Request) {
	nodeID, ok := parseTrailingID(request.URL.Path, "/api/hysteria/auth/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	token := strings.TrimSpace(request.URL.Query().Get("token"))
	if !a.hysteria.validateNodeAuthToken(request.Context(), nodeID, token) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "msg": "invalid auth token"})
		return
	}

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
	credential := strings.TrimSpace(defaultString(toString(payload["auth"]), strings.TrimSpace(request.URL.Query().Get("auth"))))
	if credential == "" {
		credential = strings.TrimSpace(request.Header.Get("Hysteria-Auth"))
	}
	if credential == "" {
		credential = strings.TrimSpace(string(rawBody))
	}
	clientID, err := a.hysteria.authorizeClient(request.Context(), nodeID, credential)
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if err != nil {
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "msg": httpSafeError(err, "认证失败")})
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "id": clientID})
}

func (a *App) readAgentPayload(writer http.ResponseWriter, request *http.Request) (map[string]any, string, bool) {
	rawBody, err := io.ReadAll(request.Body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Agent 请求格式无效")
		return nil, "", false
	}
	if len(rawBody) == 0 {
		return map[string]any{}, "", true
	}
	payload := map[string]any{}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "Agent 请求格式无效")
		return nil, "", false
	}
	return payload, string(rawBody), true
}

func (a *App) handleAgentRegister(writer http.ResponseWriter, request *http.Request) {
	payload, rawBody, ok := a.readAgentPayload(writer, request)
	if !ok {
		return
	}
	record, err := a.agents.authenticateRequest(request.Context(), request, rawBody)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 注册失败")
		return
	}
	result, err := a.agents.register(request.Context(), record, payload, clientIP(request))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 注册失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleAgentHeartbeat(writer http.ResponseWriter, request *http.Request) {
	payload, rawBody, ok := a.readAgentPayload(writer, request)
	if !ok {
		return
	}
	record, err := a.agents.authenticateRequest(request.Context(), request, rawBody)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 心跳失败")
		return
	}
	result, err := a.agents.heartbeat(request.Context(), record, payload, clientIP(request))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 心跳失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleAgentPullTask(writer http.ResponseWriter, request *http.Request) {
	_, rawBody, ok := a.readAgentPayload(writer, request)
	if !ok {
		return
	}
	record, err := a.agents.authenticateRequest(request.Context(), request, rawBody)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 拉取任务失败")
		return
	}
	task, err := a.agents.pullTask(request.Context(), record)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 拉取任务失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"task": task}})
}

func (a *App) handleAgentCompleteTask(writer http.ResponseWriter, request *http.Request) {
	if !strings.HasSuffix(request.URL.Path, "/complete") {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	payload, rawBody, ok := a.readAgentPayload(writer, request)
	if !ok {
		return
	}
	record, err := a.agents.authenticateRequest(request.Context(), request, rawBody)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 任务回传失败")
		return
	}
	taskID, ok := parsePathIDWithSuffix(request.URL.Path, "/api/agent/tasks/", "/complete")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	result, err := a.agents.completeTask(request.Context(), record, taskID, payload)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "Agent 任务回传失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleSetupStatus(writer http.ResponseWriter, request *http.Request) {
	configured := a.config.IsConfigured()
	message := "Go 面板尚未完成初始化配置"
	mode := "fresh"
	if configured && a.db != nil {
		message = "Go 面板配置已就绪"
		mode = "ready"
	} else if configured {
		message = "数据库尚未初始化"
	}

	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data: map[string]any{
			"configured":    configured,
			"databaseReady": a.db != nil,
			"requiresSetup": !configured || a.db == nil,
			"message":       message,
			"setupMode":     mode,
		},
	})
}

func (a *App) handleAPI(writer http.ResponseWriter, request *http.Request) {
	a.writeJSON(writer, http.StatusNotImplemented, apiEnvelope{
		Success: false,
		Message: "该接口尚未迁移到 Go 面板",
	})
}

func (a *App) handleSetupInit(writer http.ResponseWriter, request *http.Request) {
	var payload setupPayload
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	config, db, err := initializeFreshSetup(request.Context(), *a.config, payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "初始化失败"))
		return
	}

	*a.config = config
	a.db = db
	a.auth = newAuthService(a.db, a.config)
	a.admins = newAdminService(a.db)
	a.agents = newAgentService(a.db, a.config)
	a.audit = newAuditService(a.db)
	a.settings = newSystemSettingsStore(a.db)
	a.jobs = newJobManager(*a.config)
	a.hysteria = newHysteriaService(a.db, a.config, a.sshKeys, a.remote, a.agents)
	a.systemHealth = newSystemHealthService(a.db, a.config)

	a.writeJSON(writer, http.StatusCreated, apiEnvelope{
		Success: true,
		Data:    computeSetupStatus(*a.config, a.db),
	})
}

func (a *App) handleLoginChallenge(writer http.ResponseWriter, request *http.Request) {
	if !ensureHTTPSOrLocal(request) {
		writeError(writer, http.StatusBadRequest, "登录 challenge 必须通过 HTTPS 访问，当前请求已被拒绝")
		return
	}
	challenge, err := a.auth.issueLoginChallenge(writer)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "获取登录 challenge 失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data: map[string]any{
			"challenge": map[string]any{
				"nonce":       challenge.Nonce,
				"expiresAt":   challenge.ExpiresAt,
				"derivedSeed": DeriveLoginChallengeSeed(challenge.Nonce, *a.config),
			},
		},
	})
}

func (a *App) handleLogin(writer http.ResponseWriter, request *http.Request) {
	if a.db == nil {
		writeError(writer, http.StatusServiceUnavailable, "系统尚未初始化，请先完成首次安装")
		return
	}
	if !ensureHTTPSOrLocal(request) {
		writeError(writer, http.StatusBadRequest, "登录必须通过 HTTPS 访问，当前请求已被拒绝")
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	username := toTrimmedString(payload["username"])
	ip := clientIP(request)
	password := toTrimmedString(payload["password"])
	if encrypted, _ := payload["password_encrypted"].(bool); encrypted {
		nonce, err := a.auth.consumeLoginChallenge(writer, request, toTrimmedString(payload["login_challenge"]))
		if err != nil {
			writeError(writer, http.StatusBadRequest, "用户名或密码不正确")
			return
		}
		password, err = DecryptLoginPassword(password, nonce, *a.config)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "用户名或密码不正确")
			return
		}
	}

	if err := a.loginProtection.assertAllowed(username, ip); err != nil {
		writeError(writer, http.StatusTooManyRequests, err.Error())
		return
	}

	user, err := a.auth.attempt(request.Context(), writer, username, password)
	if err != nil {
		_ = a.loginProtection.recordFailure(username, ip)
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "用户名或密码不正确"))
		return
	}
	_ = a.loginProtection.recordSuccess(username, ip)
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"user": user}})
}

func (a *App) handleLogout(writer http.ResponseWriter, request *http.Request) {
	a.auth.logout(writer)
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{}})
}

func (a *App) handleCurrentUser(writer http.ResponseWriter, request *http.Request) {
	if a.db == nil {
		writeError(writer, http.StatusUnauthorized, "未登录")
		return
	}
	user, err := a.auth.currentUser(request.Context(), request)
	if err != nil || user == nil {
		writeError(writer, http.StatusUnauthorized, "未登录")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"user": user}})
}

func (a *App) handleSystemHealth(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "panel.view", writer) {
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: a.systemHealth.check(request.Context())})
}

func (a *App) handleSystemSettings(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "appearance.manage", writer) {
		return
	}
	settings := a.config.SystemSettings()
	settings["login_background_url"] = a.settings.loginBackgroundURL(request.Context())
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: settings})
}

func (a *App) handleUpdateSystemSettings(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "appearance.manage", writer) {
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	if value := toTrimmedString(payload["site_title"]); value != "" {
		a.config.SiteTitle = value
	}
	publicAPIBaseURL := toTrimmedString(payload["public_api_base_url"])
	if publicAPIBaseURL != "" {
		a.config.PublicAPIBaseURL = normalizePublicAPIBaseURL(publicAPIBaseURL)
	} else {
		a.config.PublicAPIBaseURL = ""
	}
	a.config.SiteIconURL = toTrimmedString(payload["site_icon_url"])
	loginBackgroundURL := toTrimmedString(payload["login_background_url"])
	a.config.MockPanel = toBool(payload["mock_panel_enabled"], a.config.MockPanel)
	a.config.MockNodeCount = boundedInt(payload["mock_node_count"], 1, 200, a.config.MockNodeCount)
	a.config.MockUserCount = boundedInt(payload["mock_user_count"], 1, 5000, a.config.MockUserCount)
	a.config.MockRunningNodeCount = boundedInt(payload["mock_running_node_count"], 0, a.config.MockNodeCount, a.config.MockRunningNodeCount)
	a.config.MockDegradedNodeCount = boundedInt(payload["mock_degraded_node_count"], 0, a.config.MockNodeCount, a.config.MockDegradedNodeCount)
	a.config.MockStoppedNodeCount = boundedInt(payload["mock_stopped_node_count"], 0, a.config.MockNodeCount, a.config.MockStoppedNodeCount)
	a.config.MockSuspendedUserCount = boundedInt(payload["mock_suspended_user_count"], 0, a.config.MockUserCount, a.config.MockSuspendedUserCount)
	normalizeMockPanelCounts(a.config)
	a.config.BruteforceEnabled = toBool(payload["bruteforce_enabled"], a.config.BruteforceEnabled)
	a.config.BruteforceMaxAttempts = boundedInt(payload["bruteforce_max_attempts"], 1, 30, a.config.BruteforceMaxAttempts)
	a.config.BruteforceWindowMinutes = boundedInt(payload["bruteforce_window_minutes"], 1, 1440, a.config.BruteforceWindowMinutes)
	a.config.BruteforceLockMinutes = boundedInt(payload["bruteforce_lock_minutes"], 1, 1440, a.config.BruteforceLockMinutes)

	if err := a.settings.set(request.Context(), settingSiteTitle, a.config.SiteTitle); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingPublicAPIBaseURL, a.config.PublicAPIBaseURL); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSiteIconURL, a.config.SiteIconURL); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingLoginBackgroundURL, loginBackgroundURL); err != nil {
		writeError(writer, http.StatusInternalServerError, "背景图配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockPanelEnabled, strconv.FormatBool(a.config.MockPanel)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockNodeCount, strconv.Itoa(a.config.MockNodeCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockUserCount, strconv.Itoa(a.config.MockUserCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockRunningNodeCount, strconv.Itoa(a.config.MockRunningNodeCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockDegradedNodeCount, strconv.Itoa(a.config.MockDegradedNodeCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockStoppedNodeCount, strconv.Itoa(a.config.MockStoppedNodeCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingMockSuspendedUserCount, strconv.Itoa(a.config.MockSuspendedUserCount)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingBruteforceEnabled, strconv.FormatBool(a.config.BruteforceEnabled)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingBruteforceMaxAttempts, strconv.Itoa(a.config.BruteforceMaxAttempts)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingBruteforceWindowMinutes, strconv.Itoa(a.config.BruteforceWindowMinutes)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingBruteforceLockMinutes, strconv.Itoa(a.config.BruteforceLockMinutes)); err != nil {
		writeError(writer, http.StatusInternalServerError, "系统配置保存失败")
		return
	}
	a.logAudit(request.Context(), user, request, "system_settings.update", "system", "app", map[string]any{
		"site_title":           a.config.SiteTitle,
		"public_api_base_url":  a.config.PublicAPIBaseURL,
		"site_icon_url":        a.config.SiteIconURL,
		"login_background_url": loginBackgroundURL,
		"mock_panel_enabled":   a.config.MockPanel,
		"mock_node_count":      a.config.MockNodeCount,
		"mock_user_count":      a.config.MockUserCount,
	})
	settings := a.config.SystemSettings()
	settings["login_background_url"] = loginBackgroundURL
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: settings})
}

func (a *App) handleVersionCheck(writer http.ResponseWriter, request *http.Request) {
	if _, ok := requireUser(request.Context(), a.auth, writer, request); !ok {
		return
	}
	result, err := checkLatestVersion(projectRootFromConfig(*a.config))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "检查更新失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleVersionUpgrade(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "system.upgrade", writer) {
		return
	}
	result, err := upgradeLatestVersion(projectRootFromConfig(*a.config))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "升级失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "system.upgrade", "system", "app", map[string]any{
		"from":   toString(result["from"]),
		"to":     toString(result["to"]),
		"backup": toString(result["backup"]),
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleNotificationSettings(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "notification.manage", writer) {
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: a.config.NotificationSettings()})
}

func (a *App) handleUpdateNotificationSettings(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "notification.manage", writer) {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	host := toTrimmedString(payload["smtp_host"])
	fromEmail := toTrimmedString(payload["smtp_from_email"])
	notifyEmail := toTrimmedString(payload["smtp_notify_email"])
	if host != "" && !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(host) {
		writeError(writer, http.StatusBadRequest, "SMTP 服务器地址格式不正确")
		return
	}
	if fromEmail != "" && !isEmail(fromEmail) {
		writeError(writer, http.StatusBadRequest, "发件邮箱格式不正确")
		return
	}
	if notifyEmail != "" && !isEmail(notifyEmail) {
		writeError(writer, http.StatusBadRequest, "通知接收邮箱格式不正确")
		return
	}

	a.config.SMTPEnabled = toBool(payload["smtp_enabled"], a.config.SMTPEnabled)
	a.config.SMTPHost = host
	a.config.SMTPPort = boundedInt(payload["smtp_port"], 1, 65535, a.config.SMTPPort)
	a.config.SMTPEncryption = normalizeSMTPEncryption(toTrimmedString(payload["smtp_encryption"]))
	a.config.SMTPUsername = toTrimmedString(payload["smtp_username"])
	if password := toString(payload["smtp_password"]); password != "" {
		a.config.SMTPPassword = password
	}
	a.config.SMTPFromEmail = fromEmail
	a.config.SMTPFromName = defaultString(toTrimmedString(payload["smtp_from_name"]), "Hysteria2 Panel")
	a.config.SMTPNotifyEmail = notifyEmail
	if err := a.settings.set(request.Context(), settingSMTPEnabled, strconv.FormatBool(a.config.SMTPEnabled)); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPHost, a.config.SMTPHost); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPPort, strconv.Itoa(a.config.SMTPPort)); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPEncryption, a.config.SMTPEncryption); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPUsername, a.config.SMTPUsername); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPPassword, a.config.SMTPPassword); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPFromEmail, a.config.SMTPFromEmail); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPFromName, a.config.SMTPFromName); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	if err := a.settings.set(request.Context(), settingSMTPNotifyEmail, a.config.SMTPNotifyEmail); err != nil {
		writeError(writer, http.StatusInternalServerError, "通知设置保存失败")
		return
	}
	a.logAudit(request.Context(), user, request, "notification_settings.update", "system", "smtp", map[string]any{
		"smtp_enabled": a.config.SMTPEnabled,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: a.config.NotificationSettings()})
}

func (a *App) handleTestNotificationSettings(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "notification.manage", writer) {
		return
	}
	if !a.config.SMTPEnabled {
		writeError(writer, http.StatusBadRequest, "请先启用 SMTP 通知")
		return
	}
	if strings.TrimSpace(a.config.SMTPHost) == "" || strings.TrimSpace(a.config.SMTPFromEmail) == "" || strings.TrimSpace(a.config.SMTPNotifyEmail) == "" {
		writeError(writer, http.StatusBadRequest, "请先完整填写 SMTP 服务器、发件邮箱和接收邮箱")
		return
	}
	if err := sendSMTPTestMail(*a.config); err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "测试邮件发送失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "notification_settings.test", "system", "smtp", map[string]any{})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{
		Success: true,
		Data: map[string]any{
			"message": "测试邮件已发送，请检查收件箱",
		},
	})
}

func (a *App) handleAuditLogs(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "audit.view", writer) {
		return
	}
	if a.db == nil {
		writeError(writer, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	page, pageSize := readPagination(request)
	page, pageSize = max(page, 1), max(pageSize, 1)
	offset := (page - 1) * pageSize
	nodeID := readOptionalNodeID(request)

	where := ""
	args := []any{}
	if nodeID > 0 {
		where = ` WHERE ((audit_logs.target_type = 'node' AND CAST(audit_logs.target_id AS UNSIGNED) = ?) OR JSON_UNQUOTE(JSON_EXTRACT(audit_logs.details_json, '$.node_id')) = ?)`
		args = append(args, nodeID, strconv.FormatInt(nodeID, 10))
	}
	var total int
	if err := a.db.QueryRowContext(request.Context(), "SELECT COUNT(*) FROM audit_logs"+where, args...).Scan(&total); err != nil {
		writeError(writer, http.StatusInternalServerError, "审计日志读取失败")
		return
	}

	args = append(args, pageSize, offset)
	rows, err := a.db.QueryContext(request.Context(), `
		SELECT
			audit_logs.id,
			audit_logs.admin_id,
			admin_users.username,
			admin_users.display_name,
			audit_logs.action,
			audit_logs.target_type,
			audit_logs.target_id,
			audit_logs.details_json,
			audit_logs.ip_address,
			audit_logs.created_at
		FROM audit_logs
		LEFT JOIN admin_users ON admin_users.id = audit_logs.admin_id`+where+`
		ORDER BY audit_logs.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "审计日志读取失败")
		return
	}
	defer rows.Close()

	items := make([]any, 0)
	for rows.Next() {
		var id int64
		var adminID sql.NullInt64
		var adminUsername, adminDisplayName sql.NullString
		var action, targetType sql.NullString
		var targetID, detailsJSON, ipAddress sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &adminID, &adminUsername, &adminDisplayName, &action, &targetType, &targetID, &detailsJSON, &ipAddress, &createdAt); err != nil {
			writeError(writer, http.StatusInternalServerError, "审计日志读取失败")
			return
		}
		items = append(items, map[string]any{
			"id":                 id,
			"admin_id":           nullInt64Value(adminID),
			"admin_username":     nullStringValue(adminUsername),
			"admin_display_name": nullStringValue(adminDisplayName),
			"action":             nullStringValue(action),
			"target_type":        nullStringValue(targetType),
			"target_id":          nullStringValue(targetID),
			"details":            decodeJSONColumn(detailsJSON),
			"ip_address":         nullStringValue(ipAddress),
			"created_at":         formatTime(createdAt),
		})
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: paginatedResult(items, page, pageSize, total)})
}

func (a *App) handlePanelState(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "panel.view", writer) {
		return
	}
	if a.db == nil {
		writeError(writer, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	result, err := a.hysteria.panelStateForUser(request.Context(), user)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, httpSafeError(err, "面板状态读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleCurrentNode(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.view", writer) {
		return
	}
	item, err := a.hysteria.getNodeForUser(request.Context(), user, 0)
	if err != nil {
		writeError(writer, http.StatusNotFound, httpSafeError(err, "节点不存在"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"node": item}})
}

func (a *App) handleNodes(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.view", writer) {
		return
	}
	page, pageSize := readPagination(request)
	result, err := a.hysteria.listNodesPageForUser(request.Context(), user, page, pageSize)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "节点列表读取失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleCreateNode(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.hysteria.saveNode(request.Context(), payload, 0)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点创建失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "node.create", "node", toString(item["id"]), map[string]any{
		"name": item["name"],
		"host": item["host"],
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"node": item}})
}

func (a *App) handleCreateNodeAndInstall(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	node, err := a.hysteria.saveNode(request.Context(), payload, 0)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点创建失败"))
		return
	}
	nodeID := int64Value(node["id"])
	installResult, installErr := a.hysteria.install(request.Context(), resolvePublicAPIBaseURL(request, a.config), nodeID)
	if installErr != nil || intValue(installResult["exitCode"]) != 0 {
		_ = a.hysteria.deleteNode(request.Context(), nodeID)
		if installErr != nil {
			writeError(writer, http.StatusBadRequest, httpSafeError(installErr, "节点安装失败"))
			return
		}
		writeError(writer, http.StatusBadRequest, defaultString(toString(installResult["output"]), "节点安装失败"))
		return
	}
	refreshedNode, _ := a.hysteria.getNodeForUser(request.Context(), user, nodeID)
	a.logAudit(request.Context(), user, request, "node.create_install", "node", strconv.FormatInt(nodeID, 10), map[string]any{
		"name":    node["name"],
		"host":    node["host"],
		"node_id": nodeID,
	})
	a.writeJSON(writer, http.StatusCreated, apiEnvelope{Success: true, Data: map[string]any{"node": refreshedNode, "install": installResult}})
}

func (a *App) handleUploadNodeSSHKey(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	if err := request.ParseMultipartForm(maxSSHKeyFileSize * 2); err != nil {
		writeError(writer, http.StatusBadRequest, "上传内容格式不正确，请重新选择 SSH 私钥文件")
		return
	}
	file, header, err := request.FormFile("private_key")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "请选择 SSH 私钥文件")
		return
	}
	result, err := a.sshKeys.upload(file, header)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "SSH 密钥上传失败"))
		return
	}
	a.writeJSON(writer, http.StatusCreated, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleUpdateNode(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/nodes/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.hysteria.saveNode(request.Context(), payload, id)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点更新失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "node.update", "node", strconv.FormatInt(id, 10), map[string]any{
		"name":    item["name"],
		"host":    item["host"],
		"node_id": id,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"node": item}})
}

func (a *App) handleDeleteNode(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/nodes/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	if err := a.hysteria.deleteNode(request.Context(), id); err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点删除失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "node.delete", "node", strconv.FormatInt(id, 10), map[string]any{
		"node_id": id,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{}})
}

func (a *App) handleSelectNode(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "node.manage", writer) {
		return
	}
	id, ok := parsePathIDWithSuffix(request.URL.Path, "/api/nodes/", "/select")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	item, err := a.hysteria.setCurrentNode(request.Context(), id)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "当前节点切换失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "node.select", "node", strconv.FormatInt(id, 10), map[string]any{
		"name":    item["name"],
		"node_id": id,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"node": item}})
}

func (a *App) handleOnlineClients(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "logs.view", writer) {
		return
	}
	nodeID := readOptionalNodeID(request)
	page, pageSize := readPagination(request)
	result, err := a.hysteria.onlineClients(request.Context(), nodeID, page, pageSize)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "在线用户读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleStreams(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "logs.view", writer) {
		return
	}
	nodeID := readOptionalNodeID(request)
	page, pageSize := readPagination(request)
	result, err := a.hysteria.dumpStreams(request.Context(), nodeID, page, pageSize)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "连接流读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleUserTrafficStats(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.view", writer) {
		return
	}
	nodeID := readOptionalNodeID(request)
	page, pageSize := readPagination(request)
	usernames := readCSVQuery(request, "usernames")
	result, err := a.hysteria.getRealtimeTrafficStats(request.Context(), nodeID, page, pageSize, usernames)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "用户流量读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleTrafficHistory(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "panel.view", writer) {
		return
	}
	hours := 24
	if value := strings.TrimSpace(request.URL.Query().Get("hours")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			hours = parsed
		}
	}
	page, pageSize := readPagination(request)
	result, err := a.hysteria.getTrafficOverview(request.Context(), hours, page, pageSize)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "流量历史读取失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleHysteriaLogs(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "logs.view", writer) {
		return
	}
	result, err := a.hysteria.logs(request.Context(), readOptionalNodeID(request))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点日志读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleHysteriaInstall(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "service.manage", writer) {
		return
	}
	nodeID := readOptionalNodeID(request)
	apiBaseURL := resolvePublicAPIBaseURL(request, a.config)
	job, err := a.jobs.runNow("install", map[string]any{"api_base_url": apiBaseURL, "node_id": nodeID}, func() (map[string]any, error) {
		return a.hysteria.install(request.Context(), apiBaseURL, nodeID)
	})
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点安装失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "hysteria.install", "job", toString(job["id"]), map[string]any{
		"action":  "install",
		"node_id": nodeID,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"job": job}})
}

func (a *App) handleHysteriaUninstall(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "service.manage", writer) {
		return
	}
	nodeID := readOptionalNodeID(request)
	job, err := a.jobs.runNow("uninstall", map[string]any{"node_id": nodeID}, func() (map[string]any, error) {
		return a.hysteria.uninstall(request.Context(), nodeID)
	})
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "节点卸载失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "hysteria.uninstall", "job", toString(job["id"]), map[string]any{
		"action":  "uninstall",
		"node_id": nodeID,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"job": job}})
}

func (a *App) handleJob(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "job.view", writer) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/jobs/"), "/")
	if id == "" {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	job, err := a.jobs.get(id)
	if err != nil {
		writeError(writer, http.StatusNotFound, "任务不存在")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"job": job}})
}

func (a *App) handleDeployConfig(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "config.manage", writer) {
		return
	}
	result, err := a.hysteria.queueConfigDeploy(request.Context(), resolvePublicAPIBaseURL(request, a.config), readOptionalNodeID(request))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "配置下发失败"))
		return
	}
	nodeID := readOptionalNodeID(request)
	a.logAudit(request.Context(), user, request, "hysteria.deploy_config", "node", optionalIDString(nodeID), map[string]any{
		"exitCode": intValue(result["exitCode"]),
		"node_id":  nodeID,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleSyncTraffic(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "traffic.sync", writer) {
		return
	}
	result, err := a.hysteria.syncTrafficUsage(request.Context(), true, readOptionalNodeID(request))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "流量同步失败"))
		return
	}
	nodeID := readOptionalNodeID(request)
	a.logAudit(request.Context(), user, request, "hysteria.sync_traffic", "node", optionalIDString(nodeID), map[string]any{
		"exitCode":        intValue(result["exitCode"]),
		"node_id":         nodeID,
		"suspended_users": result["suspendedUsers"],
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleServiceAction(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok {
		return
	}
	action := parseAction(request.URL.Path, "/api/hysteria/service/")
	if action == "" {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	if action == "status" {
		if !requirePermission(user, "service.view", writer) {
			return
		}
		result, err := a.hysteria.serviceAction(request.Context(), action, readOptionalNodeID(request))
		if err != nil {
			writeError(writer, http.StatusBadRequest, httpSafeError(err, "服务状态读取失败"))
			return
		}
		a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
		return
	}
	if !requirePermission(user, "service.manage", writer) {
		return
	}
	result, err := a.hysteria.serviceAction(request.Context(), action, readOptionalNodeID(request))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "服务操作失败"))
		return
	}
	nodeID := readOptionalNodeID(request)
	a.logAudit(request.Context(), user, request, "hysteria.service."+action, "node", optionalIDString(nodeID), map[string]any{
		"exitCode": intValue(result["exitCode"]),
		"node_id":  nodeID,
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleUsers(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.view", writer) {
		return
	}
	page, pageSize := readPagination(request)
	result, err := a.hysteria.listUsersPageForUser(request.Context(), user, page, pageSize, strings.TrimSpace(request.URL.Query().Get("keyword")))
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "用户列表读取失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleCreateUser(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.manage", writer) {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.hysteria.createUser(request.Context(), payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "用户创建失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "user.create", "user", toString(item["id"]), map[string]any{
		"username": item["username"],
		"node_id":  item["node_id"],
	})
	a.writeJSON(writer, http.StatusCreated, apiEnvelope{Success: true, Data: map[string]any{"item": item}})
}

func (a *App) handleUpdateUser(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/users/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.hysteria.updateUser(request.Context(), id, payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "用户更新失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "user.update", "user", strconv.FormatInt(id, 10), map[string]any{
		"username": item["username"],
		"node_id":  item["node_id"],
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"item": item}})
}

func (a *App) handleDeleteUser(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/users/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	if err := a.hysteria.deleteUser(request.Context(), id); err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "用户删除失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "user.delete", "user", strconv.FormatInt(id, 10), map[string]any{})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{}})
}

func (a *App) handleUserSubscriptionInfo(writer http.ResponseWriter, request *http.Request) {
	if !strings.HasSuffix(request.URL.Path, "/subscription-info") {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "user.view", writer) {
		return
	}
	id, ok := parsePathIDWithSuffix(request.URL.Path, "/api/users/", "/subscription-info")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	result, err := a.hysteria.getUserSubscriptionInfo(request.Context(), id, resolvePublicSiteBaseURL(request, a.config))
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "订阅信息读取失败"))
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleAdmins(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "admin.manage", writer) {
		return
	}
	if a.db == nil {
		writeError(writer, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	page, pageSize := readPagination(request)
	result, err := a.admins.listPage(request.Context(), page, pageSize)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "管理员列表读取失败")
		return
	}
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: result})
}

func (a *App) handleCreateAdmin(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "admin.manage", writer) {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.admins.create(request.Context(), payload)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "管理员创建失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "admin.create", "admin", toString(item["id"]), map[string]any{
		"username": item["username"],
		"role":     item["role"],
	})
	a.writeJSON(writer, http.StatusCreated, apiEnvelope{Success: true, Data: map[string]any{"item": item}})
}

func (a *App) handleUpdateAdmin(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "admin.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/admins/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	item, err := a.admins.update(request.Context(), id, payload, user.ID)
	if err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "管理员更新失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "admin.update", "admin", strconv.FormatInt(id, 10), map[string]any{
		"username": item["username"],
		"role":     item["role"],
		"status":   item["status"],
	})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{"item": item}})
}

func (a *App) handleDeleteAdmin(writer http.ResponseWriter, request *http.Request) {
	user, ok := requireUser(request.Context(), a.auth, writer, request)
	if !ok || !requirePermission(user, "admin.manage", writer) {
		return
	}
	id, ok := parseTrailingID(request.URL.Path, "/api/admins/")
	if !ok {
		writeError(writer, http.StatusNotFound, "接口不存在")
		return
	}
	if err := a.admins.delete(request.Context(), id, user.ID); err != nil {
		writeError(writer, http.StatusBadRequest, httpSafeError(err, "管理员删除失败"))
		return
	}
	a.logAudit(request.Context(), user, request, "admin.delete", "admin", strconv.FormatInt(id, 10), map[string]any{})
	a.writeJSON(writer, http.StatusOK, apiEnvelope{Success: true, Data: map[string]any{}})
}

func (a *App) handleStatic(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(writer, request)
		return
	}

	cleanPath := path.Clean("/" + request.URL.Path)
	if cleanPath == "/" {
		if a.serveStaticFile(writer, request, "index.html") {
			return
		}
		http.NotFound(writer, request)
		return
	}

	filePath := strings.TrimPrefix(cleanPath, "/")
	resolvedPath := filepath.Join(a.config.StaticDir, filepath.FromSlash(filePath))
	info, err := os.Stat(resolvedPath)
	if err == nil && !info.IsDir() {
		http.ServeFile(writer, request, resolvedPath)
		return
	}

	if errors.Is(err, os.ErrNotExist) || err == nil {
		if a.serveStaticFile(writer, request, filePath) {
			return
		}
		if a.serveStaticFile(writer, request, "index.html") {
			return
		}
		http.NotFound(writer, request)
		return
	}

	http.Error(writer, "静态资源读取失败", http.StatusInternalServerError)
}

func (a *App) serveStaticFile(writer http.ResponseWriter, request *http.Request, fileName string) bool {
	targetPath := filepath.Join(a.config.StaticDir, filepath.FromSlash(fileName))
	info, err := os.Stat(targetPath)
	if err == nil && !info.IsDir() {
		http.ServeFile(writer, request, targetPath)
		return true
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(writer, "静态资源读取失败", http.StatusInternalServerError)
		return true
	}
	return a.serveEmbeddedStaticFile(writer, request, fileName)
}

func (a *App) serveEmbeddedStaticFile(writer http.ResponseWriter, request *http.Request, fileName string) bool {
	if !embeddedassets.HasWeb() || a.embeddedStatic == nil {
		return false
	}
	cleanName := strings.TrimPrefix(path.Clean("/"+fileName), "/")
	file, err := a.embeddedStatic.Open(cleanName)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	readSeeker, ok := file.(io.ReadSeeker)
	if !ok {
		return false
	}

	http.ServeContent(writer, request, cleanName, info.ModTime(), readSeeker)
	return true
}

func (a *App) writeJSON(writer http.ResponseWriter, status int, payload apiEnvelope) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		a.logger.Printf("write json: %v", err)
	}
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(apiEnvelope{
		Success: false,
		Message: message,
	})
}

func clientIP(request *http.Request) string {
	forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	return request.RemoteAddr
}

func readPagination(request *http.Request) (int, int) {
	page := 1
	pageSize := 10
	if value := request.URL.Query().Get("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			page = parsed
		}
	}
	if value := request.URL.Query().Get("page_size"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			pageSize = parsed
		}
	}
	return page, pageSize
}

func parseTrailingID(path string, prefix string) (int64, bool) {
	if !strings.HasPrefix(path, prefix) {
		return 0, false
	}
	value := strings.TrimPrefix(path, prefix)
	if strings.Contains(value, "/") || value == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func parsePathIDWithSuffix(path string, prefix string, suffix string) (int64, bool) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	value = strings.Trim(value, "/")
	if value == "" || strings.Contains(value, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func readOptionalNodeID(request *http.Request) int64 {
	value := strings.TrimSpace(request.URL.Query().Get("node_id"))
	if value == "" {
		return 0
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func readQueryInt(request *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(request.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func readCSVQuery(request *http.Request, key string) []string {
	value := strings.TrimSpace(request.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseAction(path string, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	value := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if value == "" || strings.Contains(value, "/") {
		return ""
	}
	return value
}

func resolvePublicBaseURL(request *http.Request) string {
	if forwardedProto := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		return forwardedProto + "://" + request.Host
	}
	if request.TLS != nil {
		return "https://" + request.Host
	}
	return "http://" + request.Host
}

func isEmail(value string) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil
}

func normalizeSMTPEncryption(value string) string {
	switch strings.TrimSpace(value) {
	case "none", "ssl", "tls":
		return strings.TrimSpace(value)
	default:
		return "tls"
	}
}

func toBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func boundedInt(value any, minValue int, maxValue int, fallback int) int {
	number := fallback
	switch typed := value.(type) {
	case float64:
		number = int(typed)
	case int:
		number = typed
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			number = parsed
		}
	}
	if number < minValue {
		return minValue
	}
	if number > maxValue {
		return maxValue
	}
	return number
}

func normalizeMockPanelCounts(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.MockNodeCount < 1 {
		cfg.MockNodeCount = 1
	}
	if cfg.MockUserCount < 1 {
		cfg.MockUserCount = 1
	}
	if cfg.MockSuspendedUserCount < 0 {
		cfg.MockSuspendedUserCount = 0
	}
	if cfg.MockSuspendedUserCount > cfg.MockUserCount {
		cfg.MockSuspendedUserCount = cfg.MockUserCount
	}

	if cfg.MockRunningNodeCount < 0 {
		cfg.MockRunningNodeCount = 0
	}
	if cfg.MockDegradedNodeCount < 0 {
		cfg.MockDegradedNodeCount = 0
	}
	if cfg.MockStoppedNodeCount < 0 {
		cfg.MockStoppedNodeCount = 0
	}

	totalStatuses := cfg.MockRunningNodeCount + cfg.MockDegradedNodeCount + cfg.MockStoppedNodeCount
	if totalStatuses > cfg.MockNodeCount {
		overflow := totalStatuses - cfg.MockNodeCount
		reduce := func(value *int) {
			if overflow <= 0 || *value <= 0 {
				return
			}
			if *value >= overflow {
				*value -= overflow
				overflow = 0
				return
			}
			overflow -= *value
			*value = 0
		}
		reduce(&cfg.MockStoppedNodeCount)
		reduce(&cfg.MockDegradedNodeCount)
		reduce(&cfg.MockRunningNodeCount)
	}
}

func (a *App) logAudit(ctx context.Context, user *authUser, request *http.Request, action string, targetType string, targetID string, details map[string]any) {
	if a.audit == nil {
		return
	}
	adminID := int64(0)
	if user != nil {
		adminID = user.ID
	}
	a.audit.log(ctx, adminID, action, targetType, targetID, details, clientIP(request))
}

func optionalIDString(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func nullInt64Value(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		a.logger.Printf("%s %s %d %s", request.Method, request.URL.Path, recorder.status, time.Since(startedAt).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
