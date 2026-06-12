package panel

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	DefaultConfigPath  = "config/panel.env"
	DefaultBindAddr    = "127.0.0.1:18080"
	defaultStaticDir   = "public"
	defaultEnv         = "production"
	defaultSessionName = "hy2_panel_session"
	defaultDBPort      = 3306
	defaultDBCharset   = "utf8mb4"
)

type Config struct {
	SourcePath              string
	BindAddr                string
	StaticDir               string
	StaticDirSetting        string
	Env                     string
	SessionName             string
	SiteTitle               string
	SiteIconURL             string
	PublicAPIBaseURL        string
	MockPanel               bool
	MockNodeCount           int
	MockUserCount           int
	MockRunningNodeCount    int
	MockDegradedNodeCount   int
	MockStoppedNodeCount    int
	MockSuspendedUserCount  int
	EncryptionKey           string
	LoginAESSeed            string
	BruteforceEnabled       bool
	BruteforceMaxAttempts   int
	BruteforceWindowMinutes int
	BruteforceLockMinutes   int
	SMTPEnabled             bool
	SMTPHost                string
	SMTPPort                int
	SMTPEncryption          string
	SMTPUsername            string
	SMTPPassword            string
	SMTPFromEmail           string
	SMTPFromName            string
	SMTPNotifyEmail         string
	DBHost                  string
	DBPort                  int
	DBName                  string
	DBUser                  string
	DBPassword              string
	DBCharset               string
}

func LoadConfig(configPath string) (Config, error) {
	configPath = resolveConfigPath(configPath)
	values, err := loadEnvFile(configPath)
	if err != nil {
		return Config{}, err
	}
	staticDirSetting := strings.TrimSpace(envString(values, "PANEL_STATIC_DIR", defaultStaticDir))

	cfg := Config{
		SourcePath:              configPath,
		BindAddr:                envString(values, "PANEL_BIND_ADDR", DefaultBindAddr),
		StaticDir:               resolveStaticDir(configPath, staticDirSetting),
		StaticDirSetting:        normalizeStaticDirSetting(staticDirSetting),
		Env:                     envString(values, "PANEL_ENV", defaultEnv),
		SessionName:             envString(values, "PANEL_SESSION_NAME", defaultSessionName),
		SiteTitle:               "Hysteria2 Panel",
		SiteIconURL:             "",
		PublicAPIBaseURL:        envString(values, "PANEL_PUBLIC_API_BASE_URL", ""),
		MockPanel:               false,
		MockNodeCount:           6,
		MockUserCount:           32,
		MockRunningNodeCount:    4,
		MockDegradedNodeCount:   1,
		MockStoppedNodeCount:    1,
		MockSuspendedUserCount:  4,
		EncryptionKey:           envString(values, "PANEL_ENCRYPTION_KEY", ""),
		LoginAESSeed:            envString(values, "PANEL_LOGIN_AES_SEED", ""),
		BruteforceEnabled:       true,
		BruteforceMaxAttempts:   5,
		BruteforceWindowMinutes: 15,
		BruteforceLockMinutes:   15,
		SMTPEnabled:             false,
		SMTPHost:                "",
		SMTPPort:                587,
		SMTPEncryption:          "tls",
		SMTPUsername:            "",
		SMTPPassword:            "",
		SMTPFromEmail:           "",
		SMTPFromName:            "Hysteria2 Panel",
		SMTPNotifyEmail:         "",
		DBHost:                  envString(values, "DB_HOST", ""),
		DBPort:                  envInt(values, "DB_PORT", defaultDBPort),
		DBName:                  envString(values, "DB_NAME", ""),
		DBUser:                  envString(values, "DB_USER", ""),
		DBPassword:              envString(values, "DB_PASSWORD", ""),
		DBCharset:               envString(values, "DB_CHARSET", defaultDBCharset),
	}

	cfg.PublicAPIBaseURL = normalizePublicAPIBaseURL(cfg.PublicAPIBaseURL)

	return cfg, nil
}

func (c Config) IsConfigured() bool {
	return c.EncryptionKey != "" && c.DBHost != "" && c.DBName != "" && c.DBUser != ""
}

func (c Config) AppSettings() map[string]string {
	return map[string]string{
		"site_title":    c.SiteTitle,
		"site_icon_url": c.SiteIconURL,
	}
}

func (c Config) SystemSettings() map[string]any {
	return map[string]any{
		"site_title":                c.SiteTitle,
		"public_api_base_url":       c.PublicAPIBaseURL,
		"site_icon_url":             c.SiteIconURL,
		"mock_panel_enabled":        c.MockPanel,
		"mock_node_count":           c.MockNodeCount,
		"mock_user_count":           c.MockUserCount,
		"mock_running_node_count":   c.MockRunningNodeCount,
		"mock_degraded_node_count":  c.MockDegradedNodeCount,
		"mock_stopped_node_count":   c.MockStoppedNodeCount,
		"mock_suspended_user_count": c.MockSuspendedUserCount,
		"bruteforce_enabled":        c.BruteforceEnabled,
		"bruteforce_max_attempts":   c.BruteforceMaxAttempts,
		"bruteforce_window_minutes": c.BruteforceWindowMinutes,
		"bruteforce_lock_minutes":   c.BruteforceLockMinutes,
	}
}

func (c Config) NotificationSettings() map[string]any {
	return map[string]any{
		"smtp_enabled":             c.SMTPEnabled,
		"smtp_host":                c.SMTPHost,
		"smtp_port":                c.SMTPPort,
		"smtp_encryption":          c.SMTPEncryption,
		"smtp_username":            c.SMTPUsername,
		"smtp_password":            "",
		"smtp_password_configured": c.SMTPPassword != "",
		"smtp_from_email":          c.SMTPFromEmail,
		"smtp_from_name":           c.SMTPFromName,
		"smtp_notify_email":        c.SMTPNotifyEmail,
	}
}

func (c Config) WritablePath() string {
	if strings.TrimSpace(c.SourcePath) != "" {
		return c.SourcePath
	}
	return resolveConfigPath(DefaultConfigPath)
}

func (c Config) ProjectRoot() string {
	return resolveProjectRoot(c.WritablePath())
}

func SaveConfigFile(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, escapeEnvValue(values[key])))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func ConfigToEnvMap(cfg Config) map[string]string {
	values := map[string]string{
		"PANEL_ENCRYPTION_KEY": cfg.EncryptionKey,
		"PANEL_LOGIN_AES_SEED": cfg.LoginAESSeed,
		"DB_HOST":              cfg.DBHost,
		"DB_NAME":              cfg.DBName,
		"DB_USER":              cfg.DBUser,
	}
	if value := strings.TrimSpace(cfg.BindAddr); value != "" && value != DefaultBindAddr {
		values["PANEL_BIND_ADDR"] = value
	}
	if value := strings.TrimSpace(cfg.PublicAPIBaseURL); value != "" {
		values["PANEL_PUBLIC_API_BASE_URL"] = value
	}
	if value := strings.TrimSpace(cfg.DBPassword); value != "" {
		values["DB_PASSWORD"] = value
	}
	if cfg.DBPort > 0 && cfg.DBPort != defaultDBPort {
		values["DB_PORT"] = strconv.Itoa(cfg.DBPort)
	}
	if value := strings.TrimSpace(cfg.DBCharset); value != "" && value != defaultDBCharset {
		values["DB_CHARSET"] = value
	}
	if value := strings.TrimSpace(cfg.Env); value != "" && value != defaultEnv {
		values["PANEL_ENV"] = value
	}
	if value := strings.TrimSpace(cfg.SessionName); value != "" && value != defaultSessionName {
		values["PANEL_SESSION_NAME"] = value
	}
	if value := stableStaticDirSetting(cfg); value != "" && value != defaultStaticDir {
		values["PANEL_STATIC_DIR"] = value
	}
	return values
}

func resolveStaticDir(configPath string, staticDir string) string {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		staticDir = defaultStaticDir
	}
	if filepath.IsAbs(staticDir) {
		if isReleaseStaticDir(staticDir) {
			staticDir = defaultStaticDir
		} else {
			return staticDir
		}
	}

	return filepath.Join(resolveProjectRoot(configPath), staticDir)
}

func stableStaticDirSetting(cfg Config) string {
	value := strings.TrimSpace(cfg.StaticDirSetting)
	if value == "" {
		value = cfg.StaticDir
	}
	return normalizeStaticDirSetting(value)
}

func normalizeStaticDirSetting(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultStaticDir
	}
	if isReleaseStaticDir(value) {
		return defaultStaticDir
	}
	return value
}

func isReleaseStaticDir(value string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "." || cleaned == "" || !filepath.IsAbs(cleaned) {
		return false
	}
	normalized := filepath.ToSlash(cleaned)
	return strings.Contains(normalized, "/releases/") && strings.HasSuffix(normalized, "/public")
}

func resolveConfigPath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	if filepath.IsAbs(configPath) {
		return filepath.Clean(configPath)
	}
	if configPath == DefaultConfigPath {
		return filepath.Join(runtimeRootFromExecutable(), configPath)
	}
	if absPath, err := filepath.Abs(configPath); err == nil {
		return absPath
	}
	return filepath.Clean(configPath)
}

func resolveProjectRoot(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		resolvedConfigPath := resolveConfigPath(configPath)
		configDir := filepath.Dir(resolvedConfigPath)
		return filepath.Clean(filepath.Join(configDir, ".."))
	}
	return runtimeRootFromExecutable()
}

func runtimeRootFromExecutable() string {
	execPath, err := os.Executable()
	if err == nil {
		if resolvedPath, resolveErr := filepath.EvalSymlinks(execPath); resolveErr == nil {
			execPath = resolvedPath
		}
		execDir := filepath.Dir(execPath)
		parentDir := filepath.Dir(execDir)
		if filepath.Base(execDir) == "panel" && filepath.Base(parentDir) == "build" {
			return filepath.Clean(filepath.Join(parentDir, ".."))
		}
		return execDir
	}
	if workdir, wdErr := os.Getwd(); wdErr == nil {
		return workdir
	}
	return "."
}

func loadEnvFile(configPath string) (map[string]string, error) {
	values := map[string]string{}
	if configPath == "" {
		return values, nil
	}

	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("open panel config: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid panel config line %d", lineNumber)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan panel config: %w", err)
	}

	return values, nil
}

func envString(fileValues map[string]string, key string, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	if value, ok := fileValues[key]; ok {
		return value
	}
	return defaultValue
}

func envInt(fileValues map[string]string, key string, defaultValue int) int {
	value := envString(fileValues, key, "")
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func envBool(fileValues map[string]string, key string, defaultValue bool) bool {
	value := strings.ToLower(envString(fileValues, key, ""))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func escapeEnvValue(value string) string {
	if value == "" {
		return `""`
	}

	needsQuote := strings.ContainsAny(value, " #\t\n\r\"'")
	if !needsQuote {
		return value
	}

	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + replacer.Replace(value) + `"`
}
