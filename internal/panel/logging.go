package panel

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

const defaultLogLevel = "DEBUG"

type logLevel int

const (
	logLevelDebug logLevel = iota
	logLevelInfo
	logLevelWarn
	logLevelError
)

type PanelLogger struct {
	mu    sync.RWMutex
	base  *log.Logger
	level logLevel
}

func DefaultLogLevel() string {
	return defaultLogLevel
}

func LogFlags() int {
	return log.LstdFlags
}

func NewPanelLogger(writer io.Writer, prefix string, flags int, level string) *PanelLogger {
	return &PanelLogger{
		base:  log.New(writer, prefix, flags),
		level: parseLogLevel(level),
	}
}

func NormalizeLogLevel(value string) string {
	switch parseLogLevel(value) {
	case logLevelDebug:
		return "DEBUG"
	case logLevelWarn:
		return "WARN"
	case logLevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

func (l *PanelLogger) SetLevel(level string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = parseLogLevel(level)
}

func (l *PanelLogger) Printf(format string, args ...interface{}) {
	l.Infof(format, args...)
}

func (l *PanelLogger) Debugf(format string, args ...interface{}) {
	l.logf(logLevelDebug, "DEBUG", format, args...)
}

func (l *PanelLogger) Infof(format string, args ...interface{}) {
	l.logf(logLevelInfo, "INFO", format, args...)
}

func (l *PanelLogger) Warnf(format string, args ...interface{}) {
	l.logf(logLevelWarn, "WARN", format, args...)
}

func (l *PanelLogger) Errorf(format string, args ...interface{}) {
	l.logf(logLevelError, "ERROR", format, args...)
}

func (l *PanelLogger) Fatalf(format string, args ...interface{}) {
	l.Errorf(format, args...)
	os.Exit(1)
}

func (l *PanelLogger) logf(level logLevel, label string, format string, args ...interface{}) {
	if l == nil {
		return
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if level < l.level {
		return
	}
	l.base.Printf("["+label+"] "+format, args...)
}

func parseLogLevel(value string) logLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "debug", "true", "1", "yes", "on":
		return logLevelDebug
	case "info", "false", "0", "no", "off":
		return logLevelInfo
	case "warn", "warning":
		return logLevelWarn
	case "error":
		return logLevelError
	default:
		return logLevelDebug
	}
}
