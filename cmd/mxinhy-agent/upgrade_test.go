package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpgradeDoesNotBlockOrDuplicateControlRequests(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	a := &agent{client: server.Client(), cfg: config{AgentInstallPath: filepath.Join(t.TempDir(), "agent")}}
	upgrade := agentUpgradeInfo{Version: "test-next", DownloadURL: server.URL}
	returned := make(chan struct{})
	go func() { a.scheduleSelfUpgrade(upgrade); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("upgrade blocks control loop")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("download not started")
	}
	a.scheduleSelfUpgrade(upgrade)
	if calls.Load() != 1 {
		t.Fatal("duplicate upgrade download")
	}
	close(release)
	released = true
	deadline := time.Now().Add(time.Second)
	for {
		a.mu.Lock()
		busy := a.selfUpgrading
		a.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed download prevents retry")
		}
		time.Sleep(time.Millisecond)
	}
	a.scheduleSelfUpgrade(upgrade)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("failed upgrade not retried")
	}
}

func TestUpgradeHTTPErrorDoesNotExposeResponseBody(t *testing.T) {
	const sensitiveBody = "signed download details must stay private"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, sensitiveBody, http.StatusBadGateway)
	}))
	defer server.Close()

	a := &agent{client: server.Client(), cfg: config{AgentInstallPath: filepath.Join(t.TempDir(), "agent")}}
	err := a.applySelfUpgrade(agentUpgradeInfo{Version: "test-next", DownloadURL: server.URL})
	if err == nil {
		t.Fatal("expected upgrade error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("unexpected upgrade error: %v", err)
	}
	if strings.Contains(err.Error(), sensitiveBody) {
		t.Fatal("upgrade error exposed response body")
	}
}

func TestNormalizeLogSource(t *testing.T) {
	if got := normalizeLogSource(" Agent "); got != logSourceAgent {
		t.Fatalf("normalizeLogSource(agent) = %q", got)
	}
	if got := normalizeLogSource("unknown"); got != logSourceService {
		t.Fatalf("normalizeLogSource(unknown) = %q", got)
	}
}

func TestTrimRecentOutputKeepsNewestCompleteLines(t *testing.T) {
	oldLine := strings.Repeat("a", 5000)
	newLine := strings.Repeat("b", 5000)
	got := trimRecentOutput(oldLine + "\n" + newLine)
	if strings.Contains(got, oldLine) {
		t.Fatal("old log content was retained")
	}
	if got != newLine {
		t.Fatalf("recent log content length = %d, want %d", len(got), len(newLine))
	}
}

func TestUpdatePanelAPIBaseURLProbesAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/healthz" {
			t.Fatalf("probe path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(configPath, []byte(`{"panel_api_base_url":"https://old.example.com/api","unknown":"preserved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agent{
		client: server.Client(),
		cfg: config{
			PanelAPIBaseURL: "https://old.example.com/api",
			AgentConfigPath: configPath,
		},
	}
	next := server.URL + "/api"
	if err := a.updatePanelAPIBaseURL(next); err != nil {
		t.Fatal(err)
	}
	if got := a.panelAPIBaseURL(); got != next {
		t.Fatalf("panel API URL = %q, want %q", got, next)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["panel_api_base_url"] != next || persisted["unknown"] != "preserved" {
		t.Fatalf("persisted config = %#v", persisted)
	}
}

func TestUpdatePanelAPIBaseURLKeepsCurrentAddressWhenProbeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "agent.json")
	const current = "https://old.example.com/api"
	if err := os.WriteFile(configPath, []byte(`{"panel_api_base_url":"`+current+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agent{
		client: server.Client(),
		cfg: config{
			PanelAPIBaseURL: current,
			AgentConfigPath: configPath,
		},
	}
	if err := a.updatePanelAPIBaseURL(server.URL + "/api"); err == nil {
		t.Fatal("expected failed health check")
	}
	if got := a.panelAPIBaseURL(); got != current {
		t.Fatalf("panel API URL changed to %q", got)
	}
}
