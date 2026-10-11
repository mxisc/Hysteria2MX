package panel

import (
	"net/http/httptest"
	"testing"
)

func TestMaskNodeSecrets(t *testing.T) {
	m := map[string]any{"ssh_password": "secret", "sudo_password": ""}
	maskNodeSecrets(m)
	if m["ssh_password"] != "" || m["ssh_password_saved"] != true || m["sudo_password_saved"] != false {
		t.Fatal("invalid masking")
	}
	maskNodeSecrets(m)
	if m["ssh_password_saved"] != true {
		t.Fatal("saved flag lost")
	}
}
func TestNodeSecretRequiresAuthentication(t *testing.T) {
	a := &App{auth: &authService{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/nodes/22/secret?field=ssh_password", nil)
	a.handleNodeSecret(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache enabled")
	}
}
