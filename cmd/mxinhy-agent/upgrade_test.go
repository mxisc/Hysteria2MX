package main

import (
	"net/http"
	"net/http/httptest"
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
