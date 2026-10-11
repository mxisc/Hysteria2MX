package main

import (
	"context"
	"mxinhy/internal/nodemonitor"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorMemoryUsesAvailableAndSwap(t *testing.T) {
	total, used, swap, swapUsed, e := parseMemory("MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 600 kB\nCached: 300 kB\nSwapTotal: 200 kB\nSwapFree: 150 kB\n")
	if e != nil || total != 1024000 || used != 409600 || swap != 204800 || swapUsed != 51200 {
		t.Fatal("wrong memory or swap calculation")
	}
	if _, _, _, _, e = parseMemory("MemTotal: 10 kB\nMemAvailable: 20 kB\n"); e == nil {
		t.Fatal("invalid memory counters accepted")
	}
}

func TestMonitorRatesResetAndFirstSample(t *testing.T) {
	a := systemSample{at: time.Now(), cpu: cpuSample{total: 100, idle: 50, cores: 2}, metrics: nodemonitor.Metrics{BootID: "a", NetworkRX: 1000, NetworkTX: 2000}}
	b := systemSample{at: a.at.Add(5 * time.Second), cpu: cpuSample{total: 200, idle: 75, cores: 2}, metrics: nodemonitor.Metrics{BootID: "a", NetworkRX: 1500, NetworkTX: 3000}}
	m := sampleRates(a, b)
	if m.CPUPercent == nil || *m.CPUPercent != 75 || m.RXBytesPerSecond == nil || *m.RXBytesPerSecond != 100 || *m.TXBytesPerSecond != 200 {
		t.Fatal("incorrect elapsed-time rates")
	}
	if m = sampleRates(systemSample{}, b); m.CPUPercent != nil || m.RXBytesPerSecond != nil {
		t.Fatal("first sample has fictitious rates")
	}
	b.metrics.NetworkRX = 1
	if m = sampleRates(a, b); m.RXBytesPerSecond != nil {
		t.Fatal("counter reset caused a rate spike")
	}
	b.metrics.BootID = "b"
	if m = sampleRates(a, b); m.CPUPercent != nil || m.RXBytesPerSecond != nil {
		t.Fatal("boot change must break continuity")
	}
}

func TestBlockedMetricsUploadDoesNotBlockCollection(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)
	a := &agent{cfg: config{PanelAPIBaseURL: server.URL}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var count atomic.Int64
	a.startMonitorWorkers(ctx, func() (systemSample, error) {
		n := count.Add(1)
		return systemSample{at: time.Now(), metrics: nodemonitor.Metrics{SampledAtMS: n}}, nil
	}, 10*time.Millisecond)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	deadline := time.Now().Add(time.Second)
	for count.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if count.Load() < 4 || a.monitoringSnapshot().SampledAtMS < 4 {
		t.Fatal("slow upload blocked sampling or snapshot reads")
	}
}
