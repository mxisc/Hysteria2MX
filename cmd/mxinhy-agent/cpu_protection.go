package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Percentages are fractions of the whole machine, not of one core.
type cpuProtectionConfig struct {
	Enabled         bool    `json:"enabled"`
	HighPercent     float64 `json:"high_percent"`
	LowPercent      float64 `json:"low_percent"`
	MaxQuotaPercent float64 `json:"max_quota_percent"`
	MinQuotaPercent float64 `json:"min_quota_percent"`
}

func (c cpuProtectionConfig) defaults() (cpuProtectionConfig, error) {
	if c.HighPercent == 0 {
		c.HighPercent = 90
	}
	if c.LowPercent == 0 {
		c.LowPercent = 70
	}
	if c.MaxQuotaPercent == 0 {
		c.MaxQuotaPercent = 80
	}
	if c.MinQuotaPercent == 0 {
		c.MinQuotaPercent = 20
	}
	if c.HighPercent > 100 || c.HighPercent <= c.LowPercent || c.LowPercent <= 0 || c.MaxQuotaPercent > 100 || c.MaxQuotaPercent <= 0 || c.MinQuotaPercent <= 0 || c.MinQuotaPercent > c.MaxQuotaPercent {
		return c, fmt.Errorf("CPU protection thresholds are invalid")
	}
	return c, nil
}

type cpuSample struct {
	total, idle uint64
	cores       int
}

func parseCPUSample(data string) (cpuSample, error) {
	var s cpuSample
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] != "cpu" {
			if strings.HasPrefix(f[0], "cpu") {
				if _, e := strconv.Atoi(strings.TrimPrefix(f[0], "cpu")); e == nil {
					s.cores++
				}
			}
			continue
		}
		if len(f) < 9 {
			return s, fmt.Errorf("incomplete CPU counters")
		}
		for i := 1; i <= 8; i++ {
			v, e := strconv.ParseUint(f[i], 10, 64)
			if e != nil {
				return s, e
			}
			s.total += v
			if i == 4 || i == 5 || i == 8 {
				s.idle += v
			}
		}
	}
	// guest counters are already included in user/nice; steal is not local CPU work.
	if s.total == 0 || s.cores == 0 {
		return s, fmt.Errorf("missing CPU counters")
	}
	return s, nil
}

func cpuPercent(prev, next cpuSample) (float64, bool) {
	if next.cores != prev.cores || next.total <= prev.total || next.idle < prev.idle {
		return 0, false
	}
	total, idle := next.total-prev.total, next.idle-prev.idle
	if idle > total {
		return 0, false
	}
	return 100 * float64(total-idle) / float64(total), true
}

type cpuController struct {
	cfg       cpuProtectionConfig
	high, low int
	quota     float64
}

func (c *cpuController) next(usage float64) (float64, bool) {
	if usage > c.cfg.HighPercent {
		c.high++
		c.low = 0
		if c.high < 3 {
			return c.quota, false
		}
		c.high = 0
		q := c.cfg.MaxQuotaPercent
		if c.quota > 0 {
			q = c.quota * .8
		}
		if q < c.cfg.MinQuotaPercent {
			q = c.cfg.MinQuotaPercent
		}
		return q, q != c.quota
	}
	c.high = 0
	if usage < c.cfg.LowPercent && c.quota > 0 {
		c.low++
		if c.low < 12 {
			return c.quota, false
		}
		c.low = 0
		q := c.quota + 5
		if q > c.cfg.MaxQuotaPercent {
			q = 0
		}
		return q, true
	}
	c.low = 0
	return c.quota, false
}

type cpuGuardState struct {
	Service  string  `json:"service"`
	Baseline float64 `json:"baseline"` // systemd percentage of one core; zero = unlimited
	Quota    float64 `json:"quota"`    // whole-machine percentage; zero = baseline
}

func guardCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("systemd resource control failed: %w", e)
	}
	return strings.TrimSpace(string(b)), nil
}

var runGuardCommand = guardCommand

func baselineQuota(service string) (float64, error) {
	out, e := runGuardCommand("show", service, "--property=CPUQuotaPerSecUSec", "--value")
	if e != nil {
		return 0, e
	}
	if out == "infinity" {
		return 0, nil
	}
	d, e := time.ParseDuration(strings.ReplaceAll(out, " ", ""))
	if e != nil {
		return 0, fmt.Errorf("cannot read existing CPU quota: %w", e)
	}
	return d.Seconds() * 100, nil
}

func saveGuardState(path string, s cpuGuardState) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}

func applyGuardQuota(s cpuGuardState, cores int) error {
	q := s.Baseline
	if s.Quota > 0 {
		q = s.Quota * float64(cores)
		if s.Baseline > 0 && q > s.Baseline {
			q = s.Baseline
		}
	}
	value := "CPUQuota="
	if q > 0 {
		value = fmt.Sprintf("CPUQuota=%.2f%%", q)
	}
	_, e := runGuardCommand("set-property", "--runtime", s.Service, value)
	return e
}

func (a *agent) startCPUProtection() error {
	if runtime.GOOS != "linux" {
		if a.cfg.CPUProtection.Enabled {
			return fmt.Errorf("CPU protection requires Linux/systemd")
		}
		return nil
	}
	cfg, e := a.cfg.CPUProtection.defaults()
	if e != nil {
		return e
	}
	path := "/run/mxinhy-agent/cpu-protection.json"
	state := cpuGuardState{Service: a.cfg.ServiceName}
	b, e := os.ReadFile(path)
	saved := e == nil
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if saved {
		if e = json.Unmarshal(b, &state); e != nil {
			return e
		}
		if state.Service != a.cfg.ServiceName {
			return fmt.Errorf("CPU protection state belongs to another service")
		}
	}
	if !cfg.Enabled {
		if saved {
			state.Quota = 0
			if e = applyGuardQuota(state, 1); e != nil {
				return e
			}
			return os.Remove(path)
		}
		return nil
	}
	if !saved {
		state.Baseline, e = baselineQuota(state.Service)
		if e != nil {
			return e
		}
		if e = saveGuardState(path, state); e != nil {
			return e
		}
	}
	data, e := os.ReadFile("/proc/stat")
	if e != nil {
		return e
	}
	prev, e := parseCPUSample(string(data))
	if e != nil {
		return e
	}
	if state.Quota > cfg.MaxQuotaPercent {
		state.Quota = cfg.MaxQuotaPercent
	}
	if e = applyGuardQuota(state, prev.cores); e != nil {
		return e
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		controller := cpuController{cfg: cfg, quota: state.Quota}
		for range ticker.C {
			data, e := os.ReadFile("/proc/stat")
			if e != nil {
				controller.high = 0
				controller.low = 0
				continue
			}
			sample, e := parseCPUSample(string(data))
			if e != nil {
				controller.high = 0
				controller.low = 0
				continue
			}
			usage, valid := cpuPercent(prev, sample)
			prev = sample
			if !valid {
				controller.high = 0
				controller.low = 0
				continue
			}
			quota, changed := controller.next(usage)
			if !changed {
				continue
			}
			candidate := state
			candidate.Quota = quota
			// Persist the original ceiling before changing systemd; survive Agent crashes.
			if e = saveGuardState(path, candidate); e == nil {
				e = applyGuardQuota(candidate, sample.cores)
			}
			if e != nil {
				_ = saveGuardState(path, state)
				writeLog("warn", "CPU protection adjustment failed; will retry")
				continue
			}
			state = candidate
			controller.quota = quota
			writeLog("info", "CPU protection: machine usage %.1f%%; service quota %.1f%% (0 restores original)", usage, quota)
		}
	}()
	writeLog("info", "CPU protection enabled: threshold %.1f%% of whole machine", cfg.HighPercent)
	return nil
}
