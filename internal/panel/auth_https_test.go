package panel

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestEnsureHTTPSOrLocal(t *testing.T) {
	for _, tc := range []struct {
		name, host, peer, proto string
		tls, want               bool
	}{
		{"direct TLS", "panel.example.com", "192.0.2.1:1234", "", true, true},
		{"local proxy", "panel.example.com", "127.0.0.1:1234", "https", false, true},
		{"IPv6 proxy", "panel.example.com", "[::1]:1234", " HTTPS ", false, true},
		{"plain HTTP", "panel.example.com", "127.0.0.1:1234", "http", false, false},
		{"missing header", "panel.example.com", "127.0.0.1:1234", "", false, false},
		{"spoofed header", "panel.example.com", "192.0.2.1:1234", "https", false, false},
		{"ambiguous header", "panel.example.com", "127.0.0.1:1234", "https,http", false, false},
		{"malformed peer", "panel.example.com", "bad", "https", false, false},
		{"localhost", "localhost:18080", "127.0.0.1:1234", "", false, true},
		{"local IPv6", "[::1]:18080", "[::1]:1234", "", false, true},
		{"localhost prefix attack", "localhost.example.com", "192.0.2.1:1234", "", false, false},
		{"IP prefix attack", "127.0.0.1.example.com", "192.0.2.1:1234", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tc.host+"/api/auth/login-challenge", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := ensureHTTPSOrLocal(r); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
