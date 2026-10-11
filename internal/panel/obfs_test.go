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

func TestObfsDisabledNodeSelectionAndPersistence(t *testing.T) {
	cfg := Config{ObfsDisabledNodeIDs: "22, 23"}
	if !cfg.nodeObfsDisabled(22) || !cfg.nodeObfsDisabled(23) || cfg.nodeObfsDisabled(2) || cfg.nodeObfsDisabled(0) {
		t.Fatal("node selection must match exact IDs")
	}
	if ConfigToEnvMap(cfg)["HYSTERIA_OBFS_DISABLED_NODES"] != cfg.ObfsDisabledNodeIDs {
		t.Fatal("saving panel configuration must retain disabled node IDs")
	}
}
