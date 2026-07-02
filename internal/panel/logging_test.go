package panel

import (
	"bytes"
	"strings"
	"testing"
)

func TestPanelLoggerFiltersByLevel(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewPanelLogger(&buffer, "", 0, "INFO")

	logger.Debugf("hidden debug")
	logger.Infof("visible info")
	logger.Errorf("visible error")

	content := buffer.String()
	if strings.Contains(content, "hidden debug") {
		t.Fatalf("debug log should be filtered at info level: %q", content)
	}
	if !strings.Contains(content, "[INFO] visible info") {
		t.Fatalf("info log missing: %q", content)
	}
	if !strings.Contains(content, "[ERROR] visible error") {
		t.Fatalf("error log missing: %q", content)
	}

	buffer.Reset()
	logger.SetLevel("DEBUG")
	logger.Debugf("visible debug")
	if !strings.Contains(buffer.String(), "[DEBUG] visible debug") {
		t.Fatalf("debug log missing after debug level enabled: %q", buffer.String())
	}
}

func TestNormalizeLogLevel(t *testing.T) {
	cases := map[string]string{
		"":        "DEBUG",
		"debug":   "DEBUG",
		"true":    "DEBUG",
		"INFO":    "INFO",
		"false":   "INFO",
		"warning": "WARN",
		"error":   "ERROR",
		"invalid": "DEBUG",
	}
	for input, want := range cases {
		if got := NormalizeLogLevel(input); got != want {
			t.Fatalf("NormalizeLogLevel(%q) = %q, want %q", input, got, want)
		}
	}
}
