package panel

import (
	"strings"
	"testing"
)

func TestDisabledObfsKeepsTLSAuthAndStats(t *testing.T) {
	node := nodeRecord{ID: 22, Name: "test", Host: "example.com", ListenPort: 443, TLSMode: "self_signed", TLSCertPath: "/cert.pem", TLSKeyPath: "/key.pem", ObfsPassword: "obfs-secret", ObfsDisabled: true}
	user := userRecord{Username: "test", AuthPassword: "auth-secret", Status: "active"}
	config := buildHysteriaConfig(node, []userRecord{user}, "https://example.com/auth")
	if strings.Contains(config, "obfs:") || !strings.Contains(config, "tls:") || !strings.Contains(config, "https://example.com/auth") || !strings.Contains(config, "secret: obfs-secret") {
		t.Fatal("disabling obfs must preserve TLS, auth, and the traffic stats fallback secret")
	}
	if strings.Contains(buildConnectionURIForNodeUser(node, user), "obfs") || strings.Contains(renderClashSubscriptionEntries([]subscriptionEntry{{Node: node, User: user}}), "obfs") {
		t.Fatal("disabled obfs must be omitted from both subscription formats")
	}
	node.ObfsDisabled = false
	if !strings.Contains(buildHysteriaConfig(node, []userRecord{user}, ""), "obfs:") || !strings.Contains(buildConnectionURIForNodeUser(node, user), "obfs=salamander") {
		t.Fatal("other nodes retain obfs by default")
	}
}

func TestNodeObfsSwitchAllowsBlankPasswordWhenOff(t *testing.T) {
	s := &hysteriaService{cfg: &Config{}}
	payload := map[string]any{"deploy_mode": "local", "tls_mode": "self_signed", "obfs_enabled": false}
	data, e := s.normalizeNodePayload(payload, nil)
	if e != nil || boolValue(data["obfs_enabled"], true) || toString(data["obfs_password"]) == "" {
		t.Fatal("disabled new node must get an internal secret without requiring an obfs password")
	}
	payload["obfs_enabled"] = true
	if _, e = s.normalizeNodePayload(payload, nil); e == nil {
		t.Fatal("enabling obfs without a password must be rejected")
	}
	current := &nodeRecord{ID: 22, DeployMode: "local", TLSMode: "self_signed", ObfsDisabled: true, ObfsPassword: "existing-secret"}
	payload["obfs_enabled"] = false
	data, e = s.normalizeNodePayload(payload, current)
	if e != nil || toString(data["obfs_password"]) != "existing-secret" {
		t.Fatal("changing switch must preserve auth/stats fallback secret")
	}
}
