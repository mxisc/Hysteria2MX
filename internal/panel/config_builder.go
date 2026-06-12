package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func buildHysteriaConfig(node nodeRecord, users []userRecord, authURL string) string {
	userLines := make([]string, 0)
	for _, user := range users {
		if !isUserSubscribable(user) {
			continue
		}
		userLines = append(userLines, fmt.Sprintf("      %s: %s", user.Username, user.AuthPassword))
	}

	userBlock := buildDisabledUserpassBlock(node)
	if len(userLines) > 0 {
		userBlock = strings.Join(userLines, "\n")
	}

	authBlock := ""
	if strings.TrimSpace(authURL) != "" {
		authBlock = fmt.Sprintf("auth:\n  type: http\n  http:\n    url: %s\n    insecure: false", authURL)
	} else {
		authBlock = "auth:\n  type: userpass\n  userpass:\n" + userBlock
	}

	tlsBlock := ""
	if normalizeTLSMode(node.TLSMode) == "self_signed" {
		tlsBlock = fmt.Sprintf("tls:\n  cert: %s\n  key: %s\n  sniGuard: disable", node.TLSCertPath, node.TLSKeyPath)
	} else {
		tlsBlock = fmt.Sprintf("acme:\n  domains:\n    - %s\n  email: %s", node.Domain, node.ACMEEmail)
	}

	trafficSecret := strings.TrimSpace(node.TrafficStatsSecret)
	if trafficSecret == "" {
		trafficSecret = node.ObfsPassword
	}
	trafficListen := strings.TrimSpace(node.TrafficStatsListen)
	if trafficListen == "" {
		trafficListen = ":9999"
	}

	trafficBlock := ""
	if trafficListen != "" {
		trafficBlock = fmt.Sprintf("trafficStats:\n  listen: %s\n  secret: %s", trafficListen, trafficSecret)
	}

	return fmt.Sprintf(`listen: :%d
%s
%s
obfs:
  type: salamander
  salamander:
    password: %s
masquerade:
  type: proxy
  proxy:
    url: %s
    rewriteHost: true
bandwidth:
  up: %d mbps
  down: %d mbps
ignoreClientBandwidth: false
%s
`, node.ListenPort, tlsBlock, authBlock, node.ObfsPassword, node.MasqueradeURL, node.BandwidthUpMbps, node.BandwidthDownMbps, trafficBlock)
}

func buildDisabledUserpassBlock(node nodeRecord) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(node.ObfsPassword) + "|" + strings.TrimSpace(node.Name) + "|" + strings.TrimSpace(node.Host)))
	return fmt.Sprintf("      __mxinhy_disabled__: %s", hex.EncodeToString(sum[:16]))
}
