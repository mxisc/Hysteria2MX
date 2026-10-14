package main

import (
	"context"
	"encoding/json"
	"mxinhy/internal/nodemonitor"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestMonitorRetryPreservesSamplesAndOrder(t *testing.T) {
	var mu sync.Mutex
	var attempts []int64
	stored := make(map[int64]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m nodemonitor.Metrics
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		attempts = append(attempts, m.SampledAtMS)
		// Simulate a saved sample whose acknowledgement was lost. Retrying must
		// preserve its timestamp so the panel's existing unique key deduplicates it.
		stored[m.SampledAtMS] = true
		if len(attempts) <= 2 {
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	a := &agent{cfg: config{PanelAPIBaseURL: server.URL}}
	queue := make(chan nodemonitor.Metrics, 3)
	now := time.Now().UnixMilli()
	for i := int64(0); i < 3; i++ {
		queue <- nodemonitor.Metrics{SampledAtMS: now - 10000 + i*5000}
	}
	close(queue)
	a.runMonitorUploader(context.Background(), queue, server.Client(), time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(stored) != 3 || len(attempts) != 5 || attempts[0] != attempts[1] || attempts[1] != attempts[2] || attempts[3] != now-5000 || attempts[4] != now {
		t.Fatalf("buffered samples lost or reordered: %v", attempts)
	}
}

func TestMonitorUploaderSkipsExpiredSample(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1) }))
	defer server.Close()
	a := &agent{cfg: config{PanelAPIBaseURL: server.URL}}
	queue := make(chan nodemonitor.Metrics, 2)
	queue <- nodemonitor.Metrics{SampledAtMS: time.Now().Add(-time.Minute).UnixMilli()}
	queue <- nodemonitor.Metrics{SampledAtMS: time.Now().UnixMilli()}
	close(queue)
	a.runMonitorUploader(context.Background(), queue, server.Client(), time.Millisecond)
	if count.Load() != 1 {
		t.Fatal("expired sample blocked fresh sample")
	}
}

func TestMonitorUploaderRetriesTransportTimeout(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			time.Sleep(30 * time.Millisecond)
		}
	}))
	defer server.Close()
	a := &agent{cfg: config{PanelAPIBaseURL: server.URL}}
	queue := make(chan nodemonitor.Metrics, 1)
	queue <- nodemonitor.Metrics{SampledAtMS: time.Now().UnixMilli()}
	close(queue)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	a.runMonitorUploader(ctx, queue, &http.Client{Timeout: 10 * time.Millisecond}, time.Millisecond)
	if ctx.Err() != nil || count.Load() < 2 {
		t.Fatal("timed-out sample was not retried successfully")
	}
}

func TestMonitorUploaderCancelsRetry(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); started <- struct{}{} }))
	defer server.Close()
	a := &agent{cfg: config{PanelAPIBaseURL: server.URL}}
	queue := make(chan nodemonitor.Metrics, 1)
	queue <- nodemonitor.Metrics{SampledAtMS: time.Now().UnixMilli()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.runMonitorUploader(ctx, queue, server.Client(), time.Hour); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked by retry delay")
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
		count.Add(1)
		return systemSample{at: time.Now(), metrics: nodemonitor.Metrics{SampledAtMS: time.Now().UnixMilli()}}, nil
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
