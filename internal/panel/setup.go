package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type setupPayload struct {
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Database         string `json:"database"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	Charset          string `json:"charset"`
	PublicAPIBaseURL string `json:"public_api_base_url"`
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
}

type setupStatus struct {
	Configured    bool   `json:"configured"`
	DatabaseReady bool   `json:"databaseReady"`
	RequiresSetup bool   `json:"requiresSetup"`
	Message       string `json:"message"`
	SetupMode     string `json:"setupMode"`
}

func computeSetupStatus(cfg Config, db *sql.DB) setupStatus {
	configured := cfg.IsConfigured()
	status := setupStatus{
		Configured:    configured,
		DatabaseReady: db != nil,
		RequiresSetup: !configured || db == nil,
		Message:       "请先完成首次安装",
		SetupMode:     "fresh",
	}
	if configured && db != nil {
		status.Message = "系统已初始化"
		status.SetupMode = "ready"
	}
	if configured && db == nil {
		status.Message = "数据库尚未初始化"
	}
	return status
}

func initializeFreshSetup(ctx context.Context, current Config, payload setupPayload) (Config, *sql.DB, error) {
	if payload.Host == "" || payload.Database == "" || payload.Username == "" {
		return Config{}, nil, errors.New("请完整填写 MySQL 主机、数据库名和用户名")
	}
	if payload.Port < 1 || payload.Port > 65535 {
		return Config{}, nil, errors.New("MySQL 端口无效")
	}
	if payload.Charset == "" {
		payload.Charset = "utf8mb4"
	}
	if payload.AdminUsername == "" || payload.AdminPassword == "" {
		return Config{}, nil, errors.New("首次安装请填写管理员账号和密码")
	}
	if len(payload.AdminPassword) < 8 {
		return Config{}, nil, errors.New("管理员密码至少需要 8 位")
	}
	if err := validateAdminUsername(payload.AdminUsername); err != nil {
		return Config{}, nil, err
	}

	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=%s&parseTime=true&multiStatements=true",
		payload.Username, payload.Password, payload.Host, payload.Port, payload.Charset)
	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		return Config{}, nil, err
	}
	defer serverDB.Close()

	serverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := serverDB.PingContext(serverCtx); err != nil {
		return Config{}, nil, err
	}

	createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s COLLATE %s",
		strings.ReplaceAll(payload.Database, "`", "``"),
		payload.Charset,
		charsetCollation(payload.Charset),
	)
	if _, err := serverDB.ExecContext(ctx, createDBSQL); err != nil {
		return Config{}, nil, err
	}

	cfg := current
	cfg.DBHost = payload.Host
	cfg.DBPort = payload.Port
	cfg.DBName = payload.Database
	cfg.DBUser = payload.Username
	cfg.DBPassword = payload.Password
	cfg.DBCharset = payload.Charset
	cfg.PublicAPIBaseURL = normalizePublicAPIBaseURL(payload.PublicAPIBaseURL)
	cfg.SiteTitle = defaultString(current.SiteTitle, "Hysteria2 Panel")
	cfg.Env = defaultString(current.Env, defaultEnv)
	cfg.SessionName = defaultString(current.SessionName, defaultSessionName)
	cfg.EncryptionKey = defaultString(current.EncryptionKey, mustRandomHex(32))
	cfg.LoginAESSeed = defaultString(current.LoginAESSeed, "9007199254740991")
	cfg.SourcePath = current.WritablePath()

	db, err := OpenDB(cfg)
	if err != nil {
		return Config{}, nil, err
	}

	ready, err := hasRequiredTables(ctx, db)
	if err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}
	if ready {
		_ = db.Close()
		return Config{}, nil, errors.New("检测到目标数据库已存在系统表，当前项目仅支持纯净首次安装，请改用空数据库后重试")
	}

	schemaPath := filepath.Join(cfg.ProjectRoot(), "database", "schema.sql")
	schemaSQL, err := os.ReadFile(schemaPath)
	if err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}
	if _, err := db.ExecContext(ctx, string(schemaSQL)); err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}
	if err := newMigrationService(cfg).ensureUpToDate(db); err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}

	passwordHash, err := hashPassword(payload.AdminPassword)
	if err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO admin_users (username, password_hash, display_name, role, status) VALUES (?, ?, ?, 'super_admin', 'active')`,
		payload.AdminUsername, passwordHash, "System Admin"); err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}
	if err := newSystemSettingsStore(db).ensureDefaults(ctx, cfg); err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}

	if err := SaveConfigFile(cfg.WritablePath(), ConfigToEnvMap(cfg)); err != nil {
		_ = db.Close()
		return Config{}, nil, err
	}

	return cfg, db, nil
}

func hasRequiredTables(ctx context.Context, db *sql.DB) (bool, error) {
	const query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('admin_users', 'server_nodes', 'hysteria_users', 'audit_logs')`
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, err
	}
	return count == 4, nil
}

func charsetCollation(charset string) string {
	switch charset {
	case "utf8mb4":
		return "utf8mb4_unicode_ci"
	case "utf8":
		return "utf8_general_ci"
	default:
		validCharset := regexp.MustCompile(`^[A-Za-z0-9_]+$`)
		if !validCharset.MatchString(charset) {
			return "utf8mb4_unicode_ci"
		}
		return charset + "_general_ci"
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func mustRandomHex(bytesLength int) string {
	value, err := randomHex(bytesLength)
	if err != nil {
		return "change-this-to-a-random-secret"
	}
	return value
}
