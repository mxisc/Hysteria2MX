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
