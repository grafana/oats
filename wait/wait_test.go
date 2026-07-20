package wait

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestUntil_SucceedsFirstTry(t *testing.T) {
	r := Until[string](context.Background(), Options{Timeout: time.Second, Interval: 10 * time.Millisecond}, func() []string {
		return nil
	})
	if !r.OK {
		t.Errorf("expected OK, got %+v", r)
	}
	if r.Iterations != 1 {
		t.Errorf("Iterations: got %d, want 1", r.Iterations)
	}
}

func TestUntil_SucceedsAfterSeveralTries(t *testing.T) {
	var n int32
	r := Until[string](context.Background(), Options{Timeout: time.Second, Interval: 5 * time.Millisecond}, func() []string {
		if atomic.AddInt32(&n, 1) < 3 {
			return []string{"not yet"}
		}
		return nil
	})
	if !r.OK {
		t.Fatalf("expected OK after retries, got %+v", r)
	}
	if r.Iterations != 3 {
		t.Errorf("Iterations: got %d, want 3", r.Iterations)
	}
}

func TestUntil_FailsAtDeadline(t *testing.T) {
	start := time.Now()
	r := Until[string](context.Background(), Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond}, func() []string {
		return []string{"never passes"}
	})
	if r.OK {
		t.Errorf("expected !OK, got %+v", r)
	}
	if len(r.LastFailures) == 0 {
		t.Errorf("LastFailures should carry the last asserter output")
	}
	// Generous upper bound: guards against a runaway loop that ignores the
	// deadline (which would be seconds), while tolerating CI scheduler jitter.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Until overshot timeout too far: %s", elapsed)
	}
}

func TestUntil_RunsAtLeastOnceEvenWithTightDeadline(t *testing.T) {
	// A tight deadline should still invoke the asserter at least once. The
	// contract is "at least once," not "zero polls under short timeouts."
	called := false
	r := Until[string](context.Background(), Options{Timeout: 5 * time.Millisecond}, func() []string {
		called = true
		return []string{"x"}
	})
	if !called {
		t.Error("asserter must be invoked at least once even under tight deadline")
	}
	if r.OK {
		t.Errorf("expected !OK, got %+v", r)
	}
}

func TestUntil_CancelledContextDoesNotPass(t *testing.T) {
	// Even when the asserter passes, an already-cancelled context must not be
	// reported as success — consistent with While.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	r := Until[string](ctx, Options{Timeout: time.Second, Interval: 5 * time.Millisecond}, func() []string {
		called = true
		return nil // passes
	})
	if !called {
		t.Error("asserter should still run once before honoring cancel")
	}
	if r.OK {
		t.Errorf("cancelled context should not pass even when asserter succeeds: %+v", r)
	}
}

func TestUntil_CancelPreservesEarlierFailures(t *testing.T) {
	// Iterations fail, then the context is cancelled and the asserter passes.
	// The run is not a success, and the earlier failures must be preserved so
	// the caller can see what was failing before cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	iter := 0
	r := Until[string](ctx, Options{Timeout: time.Second, Interval: time.Millisecond}, func() []string {
		iter++
		if iter < 3 {
			return []string{"boom"}
		}
		cancel()   // cancel, then report success on this poll
		return nil // passes, but ctx is now cancelled
	})
	if r.OK {
		t.Errorf("cancelled run should not pass: %+v", r)
	}
	if len(r.LastFailures) != 1 || r.LastFailures[0] != "boom" {
		t.Errorf("expected earlier failures preserved on cancel, got %+v", r.LastFailures)
	}
}

func TestWhile_HoldsForEntireWindow(t *testing.T) {
	start := time.Now()
	r := While[string](context.Background(), Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond}, func() []string {
		return nil // never fails
	})
	if !r.OK {
		t.Errorf("expected OK, got %+v", r)
	}
	if r.Iterations < 2 {
		t.Errorf("expected multiple polls in 30ms with 5ms interval, got %d", r.Iterations)
	}
	// Generous upper bound: guards against a runaway loop that ignores the
	// deadline (which would be seconds), while tolerating CI scheduler jitter.
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("While overshot timeout too far: %s", elapsed)
	}
}

func TestWhile_FailsOnFirstFailure(t *testing.T) {
	var n int32
	r := While[string](context.Background(), Options{Timeout: 200 * time.Millisecond, Interval: 5 * time.Millisecond}, func() []string {
		if atomic.AddInt32(&n, 1) > 3 {
			return []string{"data appeared mid-window"}
		}
		return nil
	})
	if r.OK {
		t.Fatalf("expected !OK on mid-window failure, got %+v", r)
	}
	if len(r.LastFailures) == 0 {
		t.Errorf("LastFailures should describe the breach")
	}
}

func TestUntil_ContextCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Until[string](ctx, Options{Timeout: time.Second, Interval: 5 * time.Millisecond}, func() []string {
		return []string{"x"}
	})
	if r.OK {
		t.Errorf("cancelled context should not pass: %+v", r)
	}
	if r.Iterations == 0 {
		t.Errorf("asserter should still run once before honoring cancel")
	}
}
