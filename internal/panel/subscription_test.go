package panel

import (
	"strings"
	"testing"
)

func TestRenderSubscriptionEntriesIncludesEveryNode(t *testing.T) {
	user := userRecord{
		Username:     "alice",
		AuthPassword: "secret-pass",
		Status:       "active",
	}
	entries := []subscriptionEntry{
		{
			Node: nodeRecord{
				ID:              1,
				Name:            "node-a",
				Host:            "node-a.example.com",
				ListenPort:      443,
				TLSMode:         "acme",
				Domain:          "node-a.example.com",
				ObfsPassword:    "obfs-a",
				MasqueradeURL:   "https://www.cloudflare.com",
				BandwidthUpMbps: 100,
			},
			User: user,
		},
		{
			Node: nodeRecord{
				ID:              2,
				Name:            "node-b",
				Host:            "node-b.example.com",
				ListenPort:      8443,
				TLSMode:         "acme",
				Domain:          "node-b.example.com",
				ObfsPassword:    "obfs-b",
				MasqueradeURL:   "https://www.cloudflare.com",
				BandwidthUpMbps: 100,
			},
			User: user,
		},
	}

	content := renderURISubscriptionEntries(entries)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != 2 {
		t.Fatalf("renderURISubscriptionEntries() produced %d lines, want 2: %q", len(lines), content)
	}
	if !strings.Contains(lines[0], "node-a.example.com") || !strings.Contains(lines[0], "alice%40node-a") {
		t.Fatalf("first subscription line does not describe node-a: %q", lines[0])
	}
	if !strings.Contains(lines[1], "node-b.example.com") || !strings.Contains(lines[1], "alice%40node-b") {
		t.Fatalf("second subscription line does not describe node-b: %q", lines[1])
	}
}

func TestRenderClashSubscriptionEntriesIncludesEveryNode(t *testing.T) {
	user := userRecord{
		Username:       "alice",
		AuthPassword:   "secret-pass",
		Status:         "active",
		SpeedLimitMbps: 50,
	}
	entries := []subscriptionEntry{
		{
			Node: nodeRecord{
				ID:                1,
				Name:              "node-a",
				Host:              "node-a.example.com",
				ListenPort:        443,
				TLSMode:           "self_signed",
				Domain:            "node-a.example.com",
				ObfsPassword:      "obfs-a",
				MasqueradeURL:     "https://www.cloudflare.com",
				BandwidthUpMbps:   100,
				BandwidthDownMbps: 200,
			},
			User: user,
		},
		{
			Node: nodeRecord{
				ID:                2,
				Name:              "node-b",
				Host:              "node-b.example.com",
				ListenPort:        8443,
				TLSMode:           "acme",
				Domain:            "node-b.example.com",
				ObfsPassword:      "obfs-b",
				MasqueradeURL:     "https://www.cloudflare.com",
				BandwidthUpMbps:   100,
				BandwidthDownMbps: 200,
			},
			User: user,
		},
	}

	content := renderClashSubscriptionEntries(entries)
	required := []string{
		"proxies:",
		"type: hysteria2",
		"name: \"alice@node-a\"",
		"server: \"node-a.example.com\"",
		"port: 443",
		"password: \"secret-pass\"",
		"skip-cert-verify: true",
		"obfs: salamander",
		"obfs-password: \"obfs-a\"",
		"up: \"50 Mbps\"",
		"down: \"50 Mbps\"",
		"name: \"alice@node-b\"",
		"server: \"node-b.example.com\"",
		"port: 8443",
		"proxy-groups:",
		"rules:",
		"MATCH,PROXY",
	}
	for _, item := range required {
		if !strings.Contains(content, item) {
			t.Fatalf("Clash subscription is missing %q:\n%s", item, content)
		}
	}
	if strings.Count(content, "type: hysteria2") != 2 {
		t.Fatalf("Clash subscription should contain 2 hysteria2 proxies:\n%s", content)
	}
}
