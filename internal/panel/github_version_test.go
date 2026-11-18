package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubVersionUsesRunningVersion(t *testing.T) {
	oldTransport, oldBuildVersion := http.DefaultTransport, buildVersion
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		buildVersion = oldBuildVersion
	})

	for _, tc := range []struct {
		name, installed, built, latest, current string
		update                                  bool
	}{
		{"same version", "1.0.11", "", "1.0.11", "1.0.11", false},
		{"prefixed version", " v1.0.11 ", "", "1.0.11", "1.0.11", false},
		{"stale package", "1.0.10", "v1.0.11", "1.0.11", "1.0.11", false},
		{"missing package", "", "1.0.11", "1.0.11", "1.0.11", false},
		{"older release", "1.0.11", "", "1.0.10", "1.0.11", false},
		{"newer release", "1.0.11", "", "1.0.12", "1.0.11", true},
		{"numeric comparison", "1.0.9", "", "1.0.11", "1.0.9", true},
		{"unknown version", "", "", "1.0.11", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.installed != "" {
				content, err := json.Marshal(map[string]string{"version": tc.installed})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "package.json"), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			buildVersion = tc.built
			http.DefaultTransport = releaseTestTransport{
				body:   `{"tag_name":"v` + tc.latest + `","assets":[{"name":"hysteria2-plan.tar.gz"}]}`,
				status: http.StatusOK,
			}
			result, err := checkLatestVersion(root)
			if err != nil || result["current"] != tc.current || result["hasUpdate"] != tc.update {
				t.Fatalf("result=%v error=%v", result, err)
			}
			if !tc.update {
				_, err := upgradeLatestVersion(root)
				expected := "当前已是最新版本"
				if tc.current == "unknown" {
					expected = "无法确认当前版本，请稍后重试"
				}
				if err == nil || err.Error() != expected {
					t.Fatalf("upgrade error=%v, want %q", err, expected)
				}
				backups, err := filepath.Glob(filepath.Join(root, "backup_*"))
				if err != nil || len(backups) != 0 {
					t.Fatalf("unnecessary upgrade created backups: %v, error=%v", backups, err)
				}
			}
		})
	}
}

type releaseTestTransport struct {
	body   string
	status int
}

func (r releaseTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body)), Header: make(http.Header), Request: req}, nil
}

func TestGitHubVersionRequiresPublishedRuntimeAsset(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version":"1.0.5"}`), 0600); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	for _, tc := range []struct {
		name, body string
		update     bool
	}{
		{"runtime", `{"tag_name":"v1.0.6","assets":[{"name":"hysteria2-plan.tar.gz"}]}`, true},
		{"no asset", `{"tag_name":"v1.0.6","assets":[]}`, false},
		{"prerelease", `{"tag_name":"v1.0.6","prerelease":true,"assets":[{"name":"hysteria2-plan.tar.gz"}]}`, false},
		{"draft", `{"tag_name":"v1.0.6","draft":true,"assets":[{"name":"hysteria2-plan.tar.gz"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			http.DefaultTransport = releaseTestTransport{tc.body, 200}
			result, err := checkLatestVersion(root)
			if err != nil || result["hasUpdate"] != tc.update {
				t.Fatalf("result=%v error=%v", result, err)
			}
		})
	}
	if got := runtimePackageURL("v1.0.6"); got != "https://github.com/mxisc/Hysteria2MX/releases/download/v1.0.6/hysteria2-plan.tar.gz" {
		t.Fatal(got)
	}
}
