package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveProjectRootFixedInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	configPath := filepath.Join(root, "config", "panel.env")

	got := resolveProjectRoot(configPath)
	if got != root {
		t.Fatalf("resolveProjectRoot() = %q, want %q", got, root)
	}
}

func TestResolveProjectRootReleaseSharedConfig(t *testing.T) {
	deployRoot := filepath.Join(t.TempDir(), "app")
	releaseRoot := filepath.Join(deployRoot, "releases", "abc123")
	configPath := filepath.Join(deployRoot, "shared", "config", "panel.env")
	currentLink := filepath.Join(deployRoot, "current")

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir shared config: %v", err)
	}
	if err := os.MkdirAll(releaseRoot, 0o755); err != nil {
		t.Fatalf("mkdir release root: %v", err)
	}
	if err := os.Symlink(releaseRoot, currentLink); err != nil {
		if runtime.GOOS == "windows" || os.IsPermission(err) {
			t.Skipf("skip release shared config symlink test without symlink permission: %v", err)
		}
		t.Fatalf("create current symlink: %v", err)
	}

	resolvedReleaseRoot, err := filepath.EvalSymlinks(releaseRoot)
	if err != nil {
		t.Fatalf("resolve release root: %v", err)
	}

	got := resolveProjectRoot(configPath)
	if got != resolvedReleaseRoot {
		t.Fatalf("resolveProjectRoot() = %q, want %q", got, resolvedReleaseRoot)
	}
}

func TestLoadConfigUsesUnprefixedPanelEnvKeys(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "panel.env")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := strings.Join([]string{
		"PANEL_BIND_ADDR=127.0.0.1:19000",
		"BIND_ADDR=127.0.0.1:19001",
		"LOG_LEVEL=false",
		"ENCRYPTION_KEY=secret",
		"LOGIN_AES_SEED=seed",
		"DB_HOST=127.0.0.1",
		"DB_NAME=mx",
		"DB_USER=root",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(content+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:19001" {
		t.Fatalf("BindAddr = %q, want unprefixed value", cfg.BindAddr)
	}
	if cfg.LogLevel != "INFO" {
		t.Fatalf("LogLevel = %q, want INFO", cfg.LogLevel)
	}
}

func TestConfigToEnvMapWritesUnprefixedPanelKeys(t *testing.T) {
	values := ConfigToEnvMap(Config{
		BindAddr:         "127.0.0.1:19001",
		StaticDirSetting: "assets",
		Env:              "development",
		LogLevel:         "DEBUG",
		SessionName:      "session",
		PublicAPIBaseURL: "https://panel.example.com/api",
		EncryptionKey:    "secret",
		LoginAESSeed:     "seed",
		DBHost:           "127.0.0.1",
		DBName:           "mx",
		DBUser:           "root",
	})

	for _, oldKey := range []string{"PANEL_BIND_ADDR", "PANEL_STATIC_DIR", "PANEL_ENV", "PANEL_SESSION_NAME", "PANEL_PUBLIC_API_BASE_URL", "PANEL_ENCRYPTION_KEY", "PANEL_LOGIN_AES_SEED"} {
		if _, ok := values[oldKey]; ok {
			t.Fatalf("ConfigToEnvMap wrote old key %s", oldKey)
		}
	}
	if values["BIND_ADDR"] != "127.0.0.1:19001" || values["ENCRYPTION_KEY"] != "secret" || values["LOGIN_AES_SEED"] != "seed" {
		t.Fatalf("ConfigToEnvMap missing unprefixed keys: %#v", values)
	}
}
