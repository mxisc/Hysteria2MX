package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuotaPreservesExistingCeilingAndRestores(t *testing.T) {
	original := runGuardCommand
	defer func() { runGuardCommand = original }()
	var command string
	runGuardCommand = func(args ...string) (string, error) { command = strings.Join(args, " "); return "800ms", nil }
	b, e := baselineQuota("hysteria-server")
	if e != nil || b != 80 {
		t.Fatalf("baseline %v %v", b, e)
	}
	for _, item := range []struct {
		state cpuGuardState
		cores int
		want  string
	}{
		{cpuGuardState{Service: "hysteria-server", Quota: 80}, 4, "CPUQuota=320.00%"},
		{cpuGuardState{Service: "hysteria-server", Quota: 80, Baseline: 50}, 4, "CPUQuota=50.00%"},
		{cpuGuardState{Service: "hysteria-server", Baseline: 50}, 4, "CPUQuota=50.00%"},
		{cpuGuardState{Service: "hysteria-server"}, 4, "CPUQuota="},
	} {
		if e = applyGuardQuota(item.state, item.cores); e != nil || !strings.HasSuffix(command, item.want) {
			t.Fatalf("%s %v", command, e)
		}
	}
	path := filepath.Join(t.TempDir(), "state.json")
	state := cpuGuardState{Service: "hysteria-server", Baseline: 50, Quota: 20}
	if e = saveGuardState(path, state); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var loaded cpuGuardState
	if e = json.Unmarshal(data, &loaded); e != nil || loaded != state {
		t.Fatalf("state roundtrip %v", e)
	}
}

func TestCPUAccounting(t *testing.T) {
	p, e := parseCPUSample("cpu 100 0 100 600 100 50 50 100 70 0\ncpu0 0\ncpu1 0\n")
	if e != nil {
		t.Fatal(e)
	}
	n, e := parseCPUSample("cpu 200 0 200 700 100 50 50 100 170 0\ncpu0 0\ncpu1 0\n")
	if e != nil {
		t.Fatal(e)
	}
	u, ok := cpuPercent(p, n)
	if !ok || u < 66.66 || u > 66.67 || p.total != 1100 || p.cores != 2 {
		t.Fatalf("usage=%v, sample=%+v", u, p)
	}
	n.total = p.total - 1
	if _, ok = cpuPercent(p, n); ok {
		t.Fatal("counter reset accepted")
	}
	if _, e = parseCPUSample("cpu 1 2\n"); e == nil {
		t.Fatal("malformed sample accepted")
	}
}

func TestCPUProtectionHysteresis(t *testing.T) {
	cfg, e := (cpuProtectionConfig{}).defaults()
	if e != nil {
		t.Fatal(e)
	}
	c := cpuController{cfg: cfg}
	for i := 0; i < 2; i++ {
		if _, changed := c.next(99); changed {
			t.Fatal("short spike triggered")
		}
	}
	q, changed := c.next(99)
	if !changed || q != 80 {
		t.Fatalf("first reduction %v", q)
	}
	c.quota = q
	c.next(99)
	c.next(99)
	q, changed = c.next(99)
	if !changed || q != 64 {
		t.Fatalf("second reduction %v", q)
	}
	c.quota = q
	for i := 0; i < 11; i++ {
		if _, changed = c.next(60); changed {
			t.Fatal("recovered too early")
		}
	}
	q, changed = c.next(60)
	if !changed || q != 69 {
		t.Fatalf("recovery %v", q)
	}
	c.quota = 20
	for i := 0; i < 3; i++ {
		if _, changed = c.next(100); changed {
			t.Fatal("went below floor")
		}
	}
	c.quota = 80
	for i := 0; i < 12; i++ {
		q, changed = c.next(60)
	}
	if !changed || q != 0 {
		t.Fatal("original quota not restored")
	}
}

func TestCPUProtectionRejectsBadConfig(t *testing.T) {
	for _, c := range []cpuProtectionConfig{{HighPercent: 60, LowPercent: 70}, {MaxQuotaPercent: 101}, {MinQuotaPercent: 90, MaxQuotaPercent: 80}} {
		if _, e := c.defaults(); e == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
