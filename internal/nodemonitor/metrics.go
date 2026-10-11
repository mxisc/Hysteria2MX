package nodemonitor

import (
	"fmt"
	"math"
	"time"
)

// Metrics describe the entire node, independently of proxy user traffic.
type Metrics struct {
	SampledAtMS      int64    `json:"sampled_at_ms"`
	BootID           string   `json:"boot_id"`
	CPUCores         int      `json:"cpu_cores"`
	CPUPercent       *float64 `json:"cpu_percent"`
	MemoryTotal      uint64   `json:"memory_total"`
	MemoryUsed       uint64   `json:"memory_used"`
	MemoryPercent    float64  `json:"memory_percent"`
	SwapTotal        uint64   `json:"swap_total"`
	SwapUsed         uint64   `json:"swap_used"`
	SwapPercent      float64  `json:"swap_percent"`
	DiskTotal        uint64   `json:"disk_total"`
	DiskUsed         uint64   `json:"disk_used"`
	DiskPercent      float64  `json:"disk_percent"`
	Load1            float64  `json:"load1"`
	UptimeSeconds    float64  `json:"uptime_seconds"`
	NetworkRX        uint64   `json:"network_rx"`
	NetworkTX        uint64   `json:"network_tx"`
	RXBytesPerSecond *float64 `json:"rx_bytes_per_second"`
	TXBytesPerSecond *float64 `json:"tx_bytes_per_second"`
}

func (m Metrics) Validate(now time.Time) error {
	if m.SampledAtMS < now.Add(-time.Minute).UnixMilli() || m.SampledAtMS > now.Add(time.Minute).UnixMilli() || m.CPUCores < 1 || m.CPUCores > 65536 || len(m.BootID) > 64 || m.MemoryTotal == 0 || m.MemoryUsed > m.MemoryTotal || m.SwapUsed > m.SwapTotal || m.DiskUsed > m.DiskTotal {
		return fmt.Errorf("invalid node metrics")
	}
	for _, v := range []float64{m.MemoryPercent, m.SwapPercent, m.DiskPercent} {
		if !finite(v) || v < 0 || v > 100 {
			return fmt.Errorf("invalid percentage")
		}
	}
	if m.CPUPercent != nil && (!finite(*m.CPUPercent) || *m.CPUPercent < 0 || *m.CPUPercent > 100) {
		return fmt.Errorf("invalid CPU percentage")
	}
	for _, v := range []*float64{m.RXBytesPerSecond, m.TXBytesPerSecond} {
		if v != nil && (!finite(*v) || *v < 0 || *v > 1e15) {
			return fmt.Errorf("invalid network rate")
		}
	}
	if !finite(m.Load1) || m.Load1 < 0 || !finite(m.UptimeSeconds) || m.UptimeSeconds < 0 {
		return fmt.Errorf("invalid system counters")
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
