package nodemonitor

import (
	"math"
	"testing"
	"time"
)

func TestMetricsRejectStaleInvalidCountersAndRates(t *testing.T) {
	now := time.Now()
	m := Metrics{SampledAtMS: now.UnixMilli(), CPUCores: 1, MemoryTotal: 1024, MemoryUsed: 512, MemoryPercent: 50}
	if m.Validate(now) != nil {
		t.Fatal("valid first sample rejected")
	}
	for _, change := range []func(*Metrics){func(m *Metrics) { m.SampledAtMS = now.Add(-2 * time.Minute).UnixMilli() }, func(m *Metrics) { m.MemoryUsed = 2048 }, func(m *Metrics) { m.MemoryPercent = 101 }, func(m *Metrics) { v := math.NaN(); m.CPUPercent = &v }, func(m *Metrics) { v := -1.; m.TXBytesPerSecond = &v }} {
		bad := m
		change(&bad)
		if bad.Validate(now) == nil {
			t.Fatal("invalid metrics accepted")
		}
	}
}
