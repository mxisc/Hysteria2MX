package panel

import (
	"os"
	"path/filepath"
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
