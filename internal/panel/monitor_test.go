package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeMonitorRequiresLogin(t *testing.T) {
	app := &App{auth: &authService{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/{id}/monitor", app.handleNodeMonitor)
	mux.HandleFunc("GET /api/node-monitors", app.handleMonitorNodes)
	for _, path := range []string{"/api/nodes/22/monitor", "/api/node-monitors"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatal("monitor data exposed without login")
		}
	}
}

func TestMonitorUploadRejectsOversizedBody(t *testing.T) {
	app := &App{}
	w := httptest.NewRecorder()
	app.handleAgentMetrics(w, httptest.NewRequest("POST", "/api/agent/metrics", strings.NewReader(strings.Repeat("x", 9000))))
	if w.Code != http.StatusBadRequest {
		t.Fatal("oversized body accepted")
	}
}
