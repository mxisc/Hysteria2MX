package panel

import "testing"

func TestComputeTrafficDeltaWithoutPreviousSnapshot(t *testing.T) {
	if got := computeTrafficDelta(1024, 0, false); got != 0 {
		t.Fatalf("computeTrafficDelta() = %d, want 0", got)
	}
}

func TestComputeTrafficDeltaWithMonotonicCounter(t *testing.T) {
	if got := computeTrafficDelta(4096, 1024, true); got != 3072 {
		t.Fatalf("computeTrafficDelta() = %d, want 3072", got)
	}
}

func TestComputeTrafficDeltaAfterCounterReset(t *testing.T) {
	if got := computeTrafficDelta(512, 4096, true); got != 512 {
		t.Fatalf("computeTrafficDelta() = %d, want 512", got)
	}
}

func TestBuildUserTrafficStatsItemsFiltersAndFormatsCachedPayload(t *testing.T) {
	items := buildUserTrafficStatsItems(42, map[string]any{
		"alice": map[string]any{"rx": float64(1024 * 1024), "tx": float64(512 * 1024)},
		"bob":   map[string]any{"rx": float64(10), "tx": float64(20)},
	}, []string{"alice"})

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if got := toString(items[0]["username"]); got != "alice" {
		t.Fatalf("username = %q, want alice", got)
	}
	if got := int64Value(items[0]["node_id"]); got != 42 {
		t.Fatalf("node_id = %d, want 42", got)
	}
	if got := int64Value(items[0]["rx"]); got != 1024*1024 {
		t.Fatalf("rx = %d, want %d", got, 1024*1024)
	}
	if got := toString(items[0]["rx_human"]); got != "1.00 MB" {
		t.Fatalf("rx_human = %q, want 1.00 MB", got)
	}
}
