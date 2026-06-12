// Package runner orchestrates one v2 case end-to-end: seed → poll-and-assert
// → report. It is intentionally agnostic about fixtures — the caller hands
// in already-resolved endpoints, so this layer ships before the
// fixture-lifecycle layer exists.
//
// One Runner instance handles one suite of cases sharing a fixture.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/grafana/oats/assert"
	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/seed"
	"github.com/grafana/oats/signalcmd"
	"github.com/grafana/oats/v2case"
	"github.com/grafana/oats/wait"
)

// Endpoint identifies a running stack the runner can drive. It is small on
// purpose: the v2 runner only needs to know where gcx points and where to
// seed OTLP. Everything else lives in the gcx config context.
type Endpoint struct {
	// GCXContext is the value passed to gcx --context. Required.
	GCXContext string

	// OTLPHTTP is the base URL for OTLP/HTTP seed POSTs ("http://localhost:4318").
	// Required when any case uses seed.type = inline-otlp.
	OTLPHTTP string
}

// Options configures the polling cadence and per-case deadline. Sensible
// zero-value defaults apply.
type Options struct {
	// Timeout caps how long the runner waits for any one assertion to pass.
	// Default 30s.
	Timeout time.Duration

	// Interval is the gap between assertion polls. Default 500ms.
	Interval time.Duration

	// AbsentTimeout is how long an absence assertion must hold. Default 10s.
	AbsentTimeout time.Duration

	// SeedSettleDelay is how long to wait after seeding before the first
	// assertion attempt. Helps when an upstream ingest pipeline has a known
	// minimum buffer (e.g. Loki's ~5s). Default 2s.
	SeedSettleDelay time.Duration
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Interval <= 0 {
		o.Interval = 500 * time.Millisecond
	}
	if o.AbsentTimeout <= 0 {
		o.AbsentTimeout = 10 * time.Second
	}
	if o.SeedSettleDelay < 0 {
		o.SeedSettleDelay = 2 * time.Second
	}
	if o.SeedSettleDelay == 0 {
		o.SeedSettleDelay = 2 * time.Second
	}
	return o
}

// Runner executes one or more cases against a single Endpoint, emitting
// lifecycle events to the configured Reporter. It is intended to be created
// once per suite — the gcx Executor and Reporter are reused across cases.
type Runner struct {
	exec     engine.Executor
	reporter report.Reporter
	endpoint Endpoint
	opts     Options
	seeder   *seed.Sender

	// Optional skip-when-unchanged cache. nil disables caching entirely.
	cacheStore *cache.Store
	cacheCtx   CacheContext
}

// CacheContext describes the per-run inputs that must contribute to the
// cache key. They are passed in by the caller (typically cmd/v2) rather
// than discovered by the Runner because they are global to the whole run:
// the gcx version doesn't change between cases, and a fresh "gcx --version"
// per case is wasted work.
type CacheContext struct {
	GCXVersion   string
	OatsVersion  string
	FixtureBytes []byte
	Extra        map[string]string
}

// New constructs a Runner. exec is typically a configured engine.GCX;
// rep is typically a report.TextReporter or NDJSONReporter; ep names the
// gcx context and (optionally) the OTLP endpoint for inline seeding.
func New(exec engine.Executor, rep report.Reporter, ep Endpoint, opts Options) *Runner {
	return &Runner{
		exec:     exec,
		reporter: rep,
		endpoint: ep,
		opts:     opts.withDefaults(),
		seeder:   &seed.Sender{OTLPEndpoint: ep.OTLPHTTP},
	}
}

// WithCache enables the skip-when-unchanged cache for this Runner. Cases
// whose Key has been recorded green within the TTL emit a case.skip event
// instead of running. Cases that newly pass have their Key recorded;
// cases that fail have any prior Key entry evicted so a regression is
// never masked by a stale green record.
func (r *Runner) WithCache(store *cache.Store, ctx CacheContext) *Runner {
	r.cacheStore = store
	r.cacheCtx = ctx
	return r
}

func (r *Runner) cacheKey(c *v2case.Case) cache.Key {
	yamlBytes, _ := os.ReadFile(c.SourcePath) // best-effort; nil on error
	return cache.Key{
		CaseYAML:     yamlBytes,
		FixtureBytes: r.cacheCtx.FixtureBytes,
		GCXVersion:   r.cacheCtx.GCXVersion,
		OatsVersion:  r.cacheCtx.OatsVersion,
		Extra:        r.cacheCtx.Extra,
	}
}

// RunCase runs one case. Returns true on pass, false on fail. Events are
// emitted via the Reporter; errors that prevent the case from running at
// all (e.g. inline-otlp seed without an OTLP endpoint) are also surfaced
// as case.fail events with an explanatory msg.
//
// When a cache is configured (see WithCache), a hit short-circuits to
// case.skip and returns true without running the case at all. A miss
// runs the case as usual; passes are recorded, failures evict any stale
// entry so a regression is never masked.
func (r *Runner) RunCase(ctx context.Context, c *v2case.Case) bool {
	caseStart := time.Now()
	r.reporter.Emit(report.Event{
		Type:   report.EventCaseStart,
		Case:   c.Name,
		Source: c.SourcePath,
		Ts:     caseStart,
	})

	if r.cacheStore != nil {
		key := r.cacheKey(c)
		if hit, _ := r.cacheStore.Lookup(key); hit {
			r.reporter.Emit(report.Event{
				Type:   report.EventCaseSkip,
				Case:   c.Name,
				Source: c.SourcePath,
				Msg:    "cache hit (last green run within TTL)",
			})
			return true
		}
	}

	// Seed.
	if err := r.seedCase(c); err != nil {
		r.failCase(c, "seed: "+err.Error(), "")
		r.reporter.Emit(report.Event{
			Type:       report.EventCaseFail,
			Case:       c.Name,
			DurationMs: time.Since(caseStart).Milliseconds(),
		})
		return false
	}

	if r.opts.SeedSettleDelay > 0 {
		select {
		case <-time.After(r.opts.SeedSettleDelay):
		case <-ctx.Done():
			r.failCase(c, "context cancelled during seed-settle window", "")
			r.reporter.Emit(report.Event{Type: report.EventCaseFail, Case: c.Name})
			return false
		}
	}

	// Assertions, signal by signal. A failure in any signal block fails the
	// case but we still run the others — the report shows all problems.
	ok := true
	for i := range c.Expected.Traces {
		if !r.runTrace(ctx, c, &c.Expected.Traces[i]) {
			ok = false
		}
	}
	for i := range c.Expected.Logs {
		if !r.runLog(ctx, c, &c.Expected.Logs[i]) {
			ok = false
		}
	}
	for i := range c.Expected.Metrics {
		if !r.runMetric(ctx, c, &c.Expected.Metrics[i]) {
			ok = false
		}
	}
	for i := range c.Expected.Profiles {
		if !r.runProfile(ctx, c, &c.Expected.Profiles[i]) {
			ok = false
		}
	}

	durMs := time.Since(caseStart).Milliseconds()
	if ok {
		r.reporter.Emit(report.Event{Type: report.EventCasePass, Case: c.Name, DurationMs: durMs})
		if r.cacheStore != nil {
			_ = r.cacheStore.Record(r.cacheKey(c))
		}
	} else {
		r.reporter.Emit(report.Event{Type: report.EventCaseFail, Case: c.Name, DurationMs: durMs})
		if r.cacheStore != nil {
			// Evict any prior green record so a flaky regression is not
			// masked by a stale hit on the next run.
			_ = r.cacheStore.Evict(r.cacheKey(c))
		}
	}
	return ok
}

func (r *Runner) seedCase(c *v2case.Case) error {
	switch c.Seed.Type {
	case "app":
		// External fixture is responsible for booting the app. Runner
		// assumes the app is already emitting; nothing to do here.
		return nil
	case "inline-otlp":
		if r.seeder.OTLPEndpoint == "" {
			return fmt.Errorf("inline-otlp seed requires Endpoint.OTLPHTTP")
		}
		return r.seeder.Send(toSeedPayload(c.Seed))
	}
	return fmt.Errorf("unknown seed type %q", c.Seed.Type)
}

func toSeedPayload(s v2case.Seed) seed.Payload {
	p := seed.Payload{}
	for _, t := range s.Traces {
		for _, sp := range t.Spans {
			dur, _ := time.ParseDuration(sp.Duration)
			p.Traces = append(p.Traces, seed.Trace{
				Service: t.Service,
				Span: seed.SpanFields{
					Name:     sp.Name,
					Kind:     sp.Kind,
					Duration: dur,
				},
			})
		}
	}
	for _, l := range s.Logs {
		p.Logs = append(p.Logs, seed.Log{
			Service:        l.Service,
			Body:           l.Body,
			SeverityNumber: l.SeverityNumber,
			SeverityText:   l.SeverityText,
		})
	}
	for _, m := range s.Metrics {
		p.Metrics = append(p.Metrics, seed.Metric{
			Service: m.Service,
			Name:    m.Name,
			Value:   m.Value,
		})
	}
	return p
}

// pollAssert handles the polling loop common to all signal types. The
// runner builds the gcx args and an assertEval closure; pollAssert runs
// wait.Until / wait.While accordingly.
func (r *Runner) pollAssert(
	ctx context.Context,
	c *v2case.Case,
	args []string,
	absent bool,
	evalFn func(stdout, stderr string, exit int) []assert.Failure,
) bool {
	cmdStr := signalcmd.Render(args)

	run := func() []assert.Failure {
		res, err := r.exec.Execute(ctx, args...)
		if err != nil {
			return []assert.Failure{{Rule: "exec", Detail: err.Error()}}
		}
		r.reporter.Emit(report.Event{
			Type: report.EventGCXExec,
			Case: c.Name,
			Cmd:  cmdStr,
		})
		return evalFn(res.Stdout, res.Stderr, res.ExitCode)
	}

	opts := wait.Options{Timeout: r.opts.Timeout, Interval: r.opts.Interval}
	var result wait.Result[assert.Failure]
	if absent {
		opts.Timeout = r.opts.AbsentTimeout
		result = wait.While[assert.Failure](ctx, opts, run)
	} else {
		result = wait.Until[assert.Failure](ctx, opts, run)
	}

	if result.OK {
		return true
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:   report.EventAssertFail,
			Case:   c.Name,
			Source: c.SourcePath,
			Msg:    f.Error(),
			Cmd:    cmdStr,
		})
	}
	return false
}

func (r *Runner) runTrace(ctx context.Context, c *v2case.Case, a *v2case.TraceAssertion) bool {
	args := signalcmd.Traces(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		return evalCommon(stdout, a.AssertionCommon)
	})
}

func (r *Runner) runLog(ctx context.Context, c *v2case.Case, a *v2case.LogAssertion) bool {
	args := signalcmd.Logs(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		return evalCommon(stdout, a.AssertionCommon)
	})
}

func (r *Runner) runMetric(ctx context.Context, c *v2case.Case, a *v2case.MetricAssertion) bool {
	args := signalcmd.Metrics(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		fails := evalCommon(stdout, a.AssertionCommon)
		if a.Value != "" {
			actual, err := extractMetricValue(stdout)
			if err != nil {
				fails = append(fails, assert.Failure{Rule: "value", Detail: err.Error()})
			} else {
				fails = append(fails, assert.Value(actual, a.Value)...)
			}
		}
		return fails
	})
}

func (r *Runner) runProfile(ctx context.Context, c *v2case.Case, a *v2case.ProfileAssertion) bool {
	args := signalcmd.Profiles(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		return evalCommon(stdout, a.AssertionCommon)
	})
}

// evalCommon runs the assertions that every signal type shares.
func evalCommon(stdout string, c v2case.AssertionCommon) []assert.Failure {
	var fails []assert.Failure
	fails = append(fails, assert.Contains(stdout, c.Contains)...)
	fails = append(fails, assert.NotContains(stdout, c.NotContains)...)
	fails = append(fails, assert.Regex(stdout, c.Regex)...)
	if c.Count != "" {
		fails = append(fails, assert.Count(approxRowCount(stdout), c.Count)...)
	}
	if c.Absent {
		fails = append(fails, assert.Absent(approxRowCount(stdout))...)
	}
	return fails
}

// approxRowCount counts non-empty, non-banner output lines in gcx text mode.
// It is intentionally approximate — gcx's row-counting story will mature as
// we use it, and a v2.1 enhancement can swap this for a structured-output
// path. For now, "did anything come back?" is enough for absent / count.
func approxRowCount(stdout string) int {
	n := 0
	for _, line := range strings.Split(stdout, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// Skip lines that look like table-headers / dividers in gcx text mode.
		if strings.HasPrefix(t, "─") || strings.HasPrefix(t, "═") || strings.HasPrefix(t, "+") {
			continue
		}
		n++
	}
	return n
}

// extractMetricValue parses the first numeric data point out of `gcx metrics
// query -o json` output. The schema follows gcx's JSON shape; we only look
// at the fields we need so additions don't break us.
func extractMetricValue(stdout string) (float64, error) {
	var generic struct {
		Data struct {
			Result []struct {
				Value  [2]any   `json:"value"`
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		return 0, fmt.Errorf("metric value parse: %w", err)
	}
	if len(generic.Data.Result) == 0 {
		return 0, fmt.Errorf("metric value parse: empty result")
	}
	r := generic.Data.Result[0]
	raw, ok := r.Value[1].(string)
	if !ok && len(r.Values) > 0 {
		raw, ok = r.Values[len(r.Values)-1][1].(string)
	}
	if !ok {
		return 0, fmt.Errorf("metric value parse: result point has no scalar value")
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return 0, fmt.Errorf("metric value parse: %q is not a number", raw)
	}
	return f, nil
}

func (r *Runner) failCase(c *v2case.Case, msg, cmd string) {
	r.reporter.Emit(report.Event{
		Type:   report.EventAssertFail,
		Case:   c.Name,
		Source: c.SourcePath,
		Msg:    msg,
		Cmd:    cmd,
	})
}
