package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchdogRecoversOnlyStoppedServices(t *testing.T) {
	for _, status := range []string{"active", "activating", "deactivating", "unknown", "inactive", "failed"} {
		t.Run(status, func(t *testing.T) {
			a := &agent{cfg: config{AgentConfigPath: filepath.Join(t.TempDir(), "agent.json"), ServiceName: "hysteria-server"}}
			starts := 0
			run := func(command string) (string, error) {
				if strings.Contains(command, "is-active") {
					return status, errors.New("exit 3")
				}
				starts++
				return "", nil
			}
			a.recoverServiceWith(run)
			want := 0
			if status == "inactive" || status == "failed" {
				want = 1
			}
			if starts != want {
				t.Fatalf("starts=%d want=%d", starts, want)
			}
			if err := a.setServiceStopped(true); err != nil {
				t.Fatal(err)
			}
			// A new Agent instance must preserve a manual stop across restarts.
			b := &agent{cfg: a.cfg}
			b.recoverServiceWith(run)
			if starts != want {
				t.Fatal("manual stop was overridden")
			}
			if err := b.setServiceStopped(false); err != nil {
				t.Fatal(err)
			}
			b.recoverServiceWith(run)
			if starts != 2*want {
				t.Fatal("manual start did not rearm recovery")
			}
			b.selfUninstalling = true
			b.recoverServiceWith(run)
			if starts != 2*want {
				t.Fatal("recovery ran during uninstall")
			}
		})
	}
}

func TestWatchdogIntervalBounds(t *testing.T) {
	for _, seconds := range []int{0, -1, 4, 3601} {
		if watchdogInterval(seconds) != 30*time.Second {
			t.Fatal(seconds)
		}
	}
	if watchdogInterval(60) != time.Minute {
		t.Fatal("custom interval ignored")
	}
}
