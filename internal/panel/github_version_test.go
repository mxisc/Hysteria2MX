package panel

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
