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
	r := Until[string](context.Background(), Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond}, func() []string {
		return []string{"never passes"}
	})
	if r.OK {
		t.Errorf("expected !OK, got %+v", r)
	}
	if len(r.LastFailures) == 0 {
		t.Errorf("LastFailures should carry the last asserter output")
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

func TestWhile_HoldsForEntireWindow(t *testing.T) {
	r := While[string](context.Background(), Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond}, func() []string {
		return nil // never fails
	})
	if !r.OK {
		t.Errorf("expected OK, got %+v", r)
	}
	if r.Iterations < 2 {
		t.Errorf("expected multiple polls in 30ms with 5ms interval, got %d", r.Iterations)
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
