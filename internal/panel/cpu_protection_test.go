package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCPUProtectionRequiresAuthentication(t *testing.T) {
	app := &App{auth: &authService{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/{id}/cpu-protection", app.handleCPUProtection)
	mux.HandleFunc("PUT /api/nodes/{id}/cpu-protection", app.handleCPUProtection)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		r := httptest.NewRequest(method, "/api/nodes/1/cpu-protection", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s allowed without login: %d", method, w.Code)
		}
	}
	if requirePermission(&authUser{Role: "viewer"}, "node.manage", httptest.NewRecorder()) {
		t.Fatal("viewer may change protection")
	}
}
