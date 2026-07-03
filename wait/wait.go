// Package wait holds OATS v2's polling primitive: "keep trying until this
// passes, or give up at the deadline."
//
// In v1 this was gomega.Eventually. v2 drops gomega, so wait provides the
// same shape (asserter callback + timeout + polling interval) without the
// DSL. Failures are values, not panics; the runner decides how to render
// them via the report package.
//
// Two modes:
//
//	Until — succeed once any iteration's assertion has no failures
//	While — succeed only if every iteration's assertion has no failures
//	        for the entire window (used for absence checks)
package wait

import (
	"context"
	"time"
)

// Defaults applied when the caller passes zero values.
const (
	DefaultTimeout  = 30 * time.Second
	DefaultInterval = 500 * time.Millisecond
)

// Asserter is invoked on each poll. It returns the failures it observed —
// an empty slice (or nil) means "the expectation held on this iteration."
type Asserter[F any] func() []F

// Options controls polling cadence. Timeout caps the total wall-clock spent
// retrying. Interval is the gap between polls. Zero values get sensible
// defaults (see DefaultTimeout / DefaultInterval).
type Options struct {
	Timeout  time.Duration
	Interval time.Duration
}

// Result is what Until and While return. Iterations counts how many poll
// attempts ran; LastFailures is the most recent failure set observed (empty
// on Until success, possibly populated on While success when the asserter
// reports transient failures the caller chose to ignore).
type Result[F any] struct {
	OK           bool
	Iterations   int
	Elapsed      time.Duration
	LastFailures []F
}

// Until polls the asserter until it returns no failures (success) or the
// deadline elapses (failure). It runs the asserter at least once even when
// the deadline has already passed.
func Until[F any](ctx context.Context, opts Options, asserter Asserter[F]) Result[F] {
	opts = withDefaults(opts)
	start := time.Now()
	deadline := start.Add(opts.Timeout)
	var timer *time.Timer
	defer stopTimer(timer)

	var last []F
	iter := 0
	for {
		iter++
		last = asserter()
		if len(last) == 0 {
			return Result[F]{OK: true, Iterations: iter, Elapsed: time.Since(start), LastFailures: nil}
		}
		if !time.Now().Before(deadline) {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: last}
		}
		if ctx.Err() != nil {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: last}
		}
		if !waitForNextPoll(ctx, &timer, sleepInterval(opts.Interval, deadline)) {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: last}
		}
	}
}

// While polls the asserter for the entire window, requiring no failures on
// every iteration. Used for absence checks ("data we DON'T want must stay
// absent for at least N seconds"). Reports the first failing iteration's
// failures, if any.
func While[F any](ctx context.Context, opts Options, asserter Asserter[F]) Result[F] {
	opts = withDefaults(opts)
	start := time.Now()
	deadline := start.Add(opts.Timeout)
	var timer *time.Timer
	defer stopTimer(timer)

	iter := 0
	for {
		iter++
		fails := asserter()
		if len(fails) > 0 {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: fails}
		}
		if !time.Now().Before(deadline) {
			return Result[F]{OK: true, Iterations: iter, Elapsed: time.Since(start), LastFailures: nil}
		}
		if ctx.Err() != nil {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: nil}
		}
		if !waitForNextPoll(ctx, &timer, sleepInterval(opts.Interval, deadline)) {
			return Result[F]{OK: false, Iterations: iter, Elapsed: time.Since(start), LastFailures: nil}
		}
	}
}

func withDefaults(o Options) Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	return o
}

func sleepInterval(interval time.Duration, deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if remaining < interval {
		return remaining
	}
	return interval
}

func waitForNextPoll(ctx context.Context, timer **time.Timer, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if *timer == nil {
		*timer = time.NewTimer(d)
	} else {
		if !(*timer).Stop() {
			select {
			case <-(*timer).C:
			default:
			}
		}
		(*timer).Reset(d)
	}
	select {
	case <-(*timer).C:
		return true
	case <-ctx.Done():
		if !(*timer).Stop() {
			select {
			case <-(*timer).C:
			default:
			}
		}
		return false
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
