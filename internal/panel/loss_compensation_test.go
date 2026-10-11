package panel

import (
	"strings"
	"testing"
)

func TestLossCompensationConfiguration(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		node := nodeRecord{DisableLossCompensation: disabled, BandwidthUpMbps: 200, BandwidthDownMbps: 200}
		config := buildHysteriaConfig(node, nil, "")
		expected := "disableLossCompensation: false"
		if disabled {
			expected = "disableLossCompensation: true"
		}
		if !strings.Contains(config, expected) {
			t.Fatalf("missing %s", expected)
		}
		if !strings.Contains(config, "up: 200 mbps") || !strings.Contains(config, "down: 200 mbps") {
			t.Fatal("bandwidth changed")
		}
	}
}
