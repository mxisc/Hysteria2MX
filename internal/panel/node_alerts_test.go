package panel

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNodeSampleAge(t *testing.T) {
	now := int64(1_000_000)
	for _, tc := range []struct {
		sample, created int64
		stale           bool
	}{
		{now - 119999, 1, false}, {now - 120000, 1, true},
		{0, now - 119999, false}, {0, now - 120000, true}, {0, 0, false},
		{now + 5000, 1, false}, {now - 300000, 1, true},
	} {
		if got := nodeSampleStale(now, tc.sample, tc.created, 120*time.Second); got != tc.stale {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestNodeAlertLifecycle(t *testing.T) {
	now := time.Unix(10000, 0)
	cfg := Config{SMTPEnabled: true, NodeAlertEnabled: true, NodeAlertTimeoutSeconds: 120, SMTPNotifyEmail: "admin@example.com"}
	n := alertNode{id: 1, name: "测试节点", lastSample: now.Add(-3 * time.Minute).UnixMilli()}
	calls := 0
	fail := false
	send := func(Config, string, string, string) error {
		calls++
		if fail {
			return errors.New("unavailable")
		}
		return nil
	}
	state, changed, err := advanceNodeAlert(cfg, n, nodeAlertState{}, now, send)
	if err != nil || !changed || !state.Notified || calls != 1 {
		t.Fatal("missing initial alert")
	}
	// State survives JSON persistence and restart.
	data, _ := json.Marshal(state)
	state = nodeAlertState{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	_, changed, _ = advanceNodeAlert(cfg, n, state, now.Add(time.Minute), send)
	if changed || calls != 1 {
		t.Fatal("duplicate outage alert")
	}
	n.lastSample = now.UnixMilli()
	state, changed, _ = advanceNodeAlert(cfg, n, state, now, send)
	if !changed || state.Notified {
		t.Fatal("recovery did not reset incident")
	}
	n.lastSample = now.Add(-3 * time.Minute).UnixMilli()
	fail = true
	state, _, err = advanceNodeAlert(cfg, n, state, now, send)
	if err == nil || state.Notified {
		t.Fatal("failed send suppressed retry")
	}
	_, changed, _ = advanceNodeAlert(cfg, n, state, now.Add(time.Minute), send)
	if changed || calls != 2 {
		t.Fatal("retry storm")
	}
	fail = false
	state, _, err = advanceNodeAlert(cfg, n, state, now.Add(5*time.Minute), send)
	if err != nil || !state.Notified || calls != 3 {
		t.Fatal("retry did not send")
	}
	cfg.NodeAlertEnabled = false
	_, changed, _ = advanceNodeAlert(cfg, n, nodeAlertState{}, now, send)
	if changed || calls != 3 {
		t.Fatal("disabled alert sent")
	}
}
