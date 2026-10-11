package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mxinhy/internal/nodemonitor"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const monitorInterval = 5 * time.Second

type systemSample struct {
	cpu     cpuSample
	metrics nodemonitor.Metrics
	at      time.Time
}

func parseMemory(data string) (total, used, swapTotal, swapUsed uint64, err error) {
	values := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, e := strconv.ParseUint(f[1], 10, 64)
		if e != nil {
			return 0, 0, 0, 0, e
		}
		values[strings.TrimSuffix(f[0], ":")] = v * 1024
	}
	total = values["MemTotal"]
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if total == 0 || available > total {
		return 0, 0, 0, 0, fmt.Errorf("invalid memory counters")
	}
	used = total - available
	swapTotal = values["SwapTotal"]
	if values["SwapFree"] > swapTotal {
		return 0, 0, 0, 0, fmt.Errorf("invalid swap counters")
	}
	swapUsed = swapTotal - values["SwapFree"]
	return
}

func parseNetwork(data string) (rx, tx uint64, err error) {
	for _, line := range strings.Split(data, "\n") {
		name, counters, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		f := strings.Fields(counters)
		if len(f) < 16 {
			return 0, 0, fmt.Errorf("invalid network counters")
		}
		r, e := strconv.ParseUint(f[0], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		t, e := strconv.ParseUint(f[8], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		rx += r
		tx += t
	}
	return
}

func readSystemSample() (systemSample, error) {
	s := systemSample{at: time.Now()}
	read := func(path string) (string, error) { b, e := os.ReadFile(path); return string(b), e }
	data, e := read("/proc/stat")
	if e != nil {
		return s, e
	}
	s.cpu, e = parseCPUSample(data)
	if e != nil {
		return s, e
	}
	m := &s.metrics
	m.SampledAtMS = s.at.UnixMilli()
	m.CPUCores = s.cpu.cores
	data, e = read("/proc/meminfo")
	if e != nil {
		return s, e
	}
	m.MemoryTotal, m.MemoryUsed, m.SwapTotal, m.SwapUsed, e = parseMemory(data)
	if e != nil {
		return s, e
	}
	m.MemoryPercent = 100 * float64(m.MemoryUsed) / float64(m.MemoryTotal)
	if m.SwapTotal > 0 {
		m.SwapPercent = 100 * float64(m.SwapUsed) / float64(m.SwapTotal)
	}
	data, e = read("/proc/net/dev")
	if e != nil {
		return s, e
	}
	m.NetworkRX, m.NetworkTX, e = parseNetwork(data)
	if e != nil {
		return s, e
	}
	data, e = read("/proc/uptime")
	if e != nil {
		return s, e
	}
	f := strings.Fields(data)
	if len(f) < 1 {
		return s, fmt.Errorf("missing uptime")
	}
	m.UptimeSeconds, e = strconv.ParseFloat(f[0], 64)
	if e != nil {
		return s, e
	}
	data, e = read("/proc/loadavg")
	if e != nil {
		return s, e
	}
	f = strings.Fields(data)
	if len(f) < 1 {
		return s, fmt.Errorf("missing load")
	}
	m.Load1, e = strconv.ParseFloat(f[0], 64)
	if e != nil {
		return s, e
	}
	data, _ = read("/proc/sys/kernel/random/boot_id")
	m.BootID = strings.TrimSpace(data)
	m.DiskTotal, m.DiskUsed = rootDiskUsage()
	if m.DiskTotal > 0 {
		m.DiskPercent = 100 * float64(m.DiskUsed) / float64(m.DiskTotal)
	}
	return s, nil
}

func sampleRates(previous, next systemSample) nodemonitor.Metrics {
	m := next.metrics
	if previous.at.IsZero() || previous.metrics.BootID != next.metrics.BootID {
		return m
	}
	if v, ok := cpuPercent(previous.cpu, next.cpu); ok {
		m.CPUPercent = &v
	}
	seconds := next.at.Sub(previous.at).Seconds()
	if seconds > 0 && m.NetworkRX >= previous.metrics.NetworkRX && m.NetworkTX >= previous.metrics.NetworkTX {
		rx := float64(m.NetworkRX-previous.metrics.NetworkRX) / seconds
		tx := float64(m.NetworkTX-previous.metrics.NetworkTX) / seconds
		m.RXBytesPerSecond = &rx
		m.TXBytesPerSecond = &tx
	}
	return m
}

// Collection and upload have separate workers and a one-sample queue. Slow or
// unavailable panels cannot block sampling, heartbeat, or command processing.
func (a *agent) startMonitoring(ctx context.Context) {
	a.startMonitorWorkers(ctx, readSystemSample, monitorInterval)
}

func (a *agent) startMonitorWorkers(ctx context.Context, collect func() (systemSample, error), interval time.Duration) {
	queue := make(chan nodemonitor.Metrics, 1)
	go func() {
		client := &http.Client{Timeout: 4 * time.Second}
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-queue:
				_ = a.uploadMetrics(ctx, client, m) // The next sample retries; stale state is visible in the panel.
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var previous systemSample
		for {
			s, e := collect()
			if e == nil {
				m := sampleRates(previous, s)
				previous = s
				a.monitorMu.Lock()
				a.latestMetrics = &m
				a.monitorMu.Unlock()
				select {
				case queue <- m:
				default:
					select {
					case <-queue:
					default:
					}
					select {
					case queue <- m:
					default:
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *agent) monitoringSnapshot() *nodemonitor.Metrics {
	a.monitorMu.RLock()
	defer a.monitorMu.RUnlock()
	if a.latestMetrics == nil {
		return nil
	}
	copy := *a.latestMetrics
	return &copy
}

func (a *agent) uploadMetrics(ctx context.Context, client *http.Client, m nodemonitor.Metrics) error {
	body, e := json.Marshal(m)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.PanelAPIBaseURL, "/")+"/agent/metrics", bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	addAgentAuthHeaders(req, a.cfg.AgentID, a.cfg.AgentSecret, body)
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("monitor upload failed")
	}
	return nil
}
