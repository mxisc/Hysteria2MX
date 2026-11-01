package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *agent) serviceStopPath() string {
	return a.cfg.AgentConfigPath + ".service-stopped"
}

func (a *agent) setServiceStopped(stopped bool) error {
	path := a.serviceStopPath()
	if stopped {
		return os.WriteFile(path, []byte("stopped\n"), 0600)
	}
	return os.WriteFile(path, []byte("running\n"), 0600)
}

func watchdogInterval(seconds int) time.Duration {
	if seconds < 5 || seconds > 3600 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func (a *agent) runServiceWatchdog(ctx context.Context) {
	ticker := time.NewTicker(watchdogInterval(a.cfg.WatchdogIntervalSeconds))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.recoverService()
		}
	}
}

func (a *agent) recoverService() {
	a.recoverServiceWith(runCommand)
}

func (a *agent) recoverServiceWith(run func(string) (string, error)) {
	a.serviceMu.Lock()
	defer a.serviceMu.Unlock()
	a.mu.Lock()
	skip := a.selfUninstalling || a.selfUpgrading
	a.mu.Unlock()
	if skip || !filepath.IsAbs(a.cfg.AgentConfigPath) {
		return
	}
	intent, err := os.ReadFile(a.serviceStopPath())
	if (err != nil && !os.IsNotExist(err)) || (err == nil && strings.TrimSpace(string(intent)) != "running") {
		return // Fail closed when operator intent cannot be read.
	}
	status, _ := run("systemctl is-active " + shellQuote(a.cfg.ServiceName))
	status = strings.TrimSpace(status)
	if status != "inactive" && status != "failed" {
		return // Do not interrupt transitions or start a missing/unknown unit.
	}
	if _, err := run("systemctl start " + shellQuote(a.cfg.ServiceName)); err != nil {
		writeLog("warn", "服务自动恢复失败，将在下一检查周期重试")
		return
	}
	writeLog("info", "已尝试自动恢复停止运行的服务")
}
