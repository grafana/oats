// Package runner orchestrates one case end-to-end: seed → poll-and-assert
// → report. It is intentionally agnostic about fixtures — the caller hands
// in already-resolved endpoints, so this layer ships before the
// fixture-lifecycle layer exists.
//
// One Runner instance handles one group of cases sharing a fixture.
package runner

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/assert"
	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/seed"
	"github.com/grafana/oats/signalcmd"
	"github.com/grafana/oats/testhelpers/requests"
	"github.com/grafana/oats/wait"
)

// Endpoint identifies a running stack the runner can drive. It is small on
// purpose: the v2 runner only needs to know where gcx points and where to
// seed OTLP. Everything else lives in the gcx config context.
type Endpoint struct {
	// GCXContext is the value passed to gcx --context when using a named gcx
	// context. Local LGTM fixtures may leave this empty and instead provide
	// GCXConfig.
	GCXContext string

	// GCXConfig is an optional gcx config file path for local fixtures.
	GCXConfig string

	// OTLPHTTP is the base URL for OTLP/HTTP seed POSTs ("http://localhost:4318").
	// Required when any case uses seed.type = inline-otlp.
	OTLPHTTP string

	// AppHost/AppPort identify the application under test for `input` request
	// driving. Individual inputs may override host or scheme, but the port
	// comes from here.
	AppHost string
	AppPort int

	// GCXEnv supplements the gcx subprocess environment for every assertion.
	// OATS uses this for local LGTM fixtures so consumer repos do not need a
	// custom gcx wrapper script just to inject Grafana auth.
	GCXEnv []string

	// CustomCheckEnv is appended to each custom-check subprocess environment.
	// It carries fixture-specific helpers like COMPOSE_FILE and stable local
	// endpoint URLs.
	CustomCheckEnv []string
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
	if o.SeedSettleDelay == 0 {
		o.SeedSettleDelay = 2 * time.Second
	}
	return o
}

// Runner executes one or more cases against a single Endpoint, emitting
// lifecycle events to the configured Reporter. It is intended to be created
// once per group — the gcx Executor and Reporter are reused across cases.
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
// cache key. They are passed in by the caller (typically the CLI package) rather
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

func (r *Runner) cacheKey(c *casefile.Case) cache.Key {
	var yamlBytes []byte
	if c.SourcePath != "" {
		if data, err := os.ReadFile(c.SourcePath); err == nil {
			yamlBytes = data
		}
	}
	if len(yamlBytes) == 0 {
		yamlBytes = []byte(fmt.Sprintf("case:%s\nsource:%s\n", c.Name, c.SourcePath))
	}
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
func (r *Runner) RunCase(ctx context.Context, c *casefile.Case) bool {
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
				Type:    report.EventCaseSkip,
				Case:    c.Name,
				Source:  c.SourcePath,
				Message: "cache hit (last green run within TTL)",
			})
			return true
		}
	}

	// Seed.
	if err := r.seedCase(ctx, c); err != nil {
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
	for _, msg := range c.Expected.ComposeLogs {
		if !r.runComposeLogCheck(ctx, c, msg) {
			ok = false
		}
	}
	for i := range c.Expected.Custom {
		if !r.runCustomCheck(ctx, c, &c.Expected.Custom[i]) {
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

func (r *Runner) seedCase(ctx context.Context, c *casefile.Case) error {
	switch c.Seed.EffectiveType() {
	case "app":
		// External fixture is responsible for booting the app. Runner
		// assumes the app is already emitting; nothing to do here.
		return nil
	case "inline-otlp":
		if r.seeder.OTLPEndpoint == "" {
			return fmt.Errorf("inline-otlp seed requires Endpoint.OTLPHTTP")
		}
		payload, err := toSeedPayload(c.Seed)
		if err != nil {
			return err
		}
		return r.seeder.Send(ctx, payload)
	}
	return fmt.Errorf("unknown seed type %q", c.Seed.Type)
}

func toSeedPayload(s casefile.Seed) (seed.Payload, error) {
	p := seed.Payload{}
	for _, t := range s.Traces {
		for _, sp := range t.Spans {
			dur := time.Duration(0)
			if sp.Duration != "" {
				parsed, err := time.ParseDuration(sp.Duration)
				if err != nil {
					return seed.Payload{}, fmt.Errorf("seed trace span %q: invalid duration %q: %w", sp.Name, sp.Duration, err)
				}
				dur = parsed
			}
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
	return p, nil
}

// pollAssert handles the polling loop common to all signal types. The
// runner builds the gcx args and an assertEval closure; pollAssert runs
// wait.Until / wait.While accordingly.
func (r *Runner) pollAssert(
	ctx context.Context,
	c *casefile.Case,
	args []string,
	absent bool,
	evalFn func(stdout, stderr string, exit int) []assert.Failure,
) bool {
	cmdStr := signalcmd.Render(args)

	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		execCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		defer cancel()
		res, err := r.exec.Execute(execCtx, args...)
		if err != nil {
			return []assert.Failure{{Rule: "exec", Detail: err.Error()}}
		}
		r.reporter.Emit(report.Event{
			Type: report.EventGCXExec,
			Case: c.Name,
			Cmd:  cmdStr,
		})
		if res.ExitCode != 0 {
			detail := trimOutput(strings.TrimSpace(res.Stderr))
			if detail == "" {
				detail = fmt.Sprintf("gcx exit code %d", res.ExitCode)
			}
			return []assert.Failure{{Rule: "exec", Detail: detail}}
		}
		return evalFn(res.Stdout, res.Stderr, res.ExitCode)
	}

	opts := wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}
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
	if len(result.LastFailures) == 0 {
		r.reporter.Emit(report.Event{
			Type:    report.EventAssertFail,
			Case:    c.Name,
			Source:  c.SourcePath,
			Message: "assertion polling stopped before any failure details were captured",
			Cmd:     cmdStr,
		})
		return false
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:    report.EventAssertFail,
			Case:    c.Name,
			Source:  c.SourcePath,
			Message: f.Error(),
			Cmd:     cmdStr,
		})
	}
	return false
}

func (r *Runner) caseInterval(c *casefile.Case) time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return r.opts.Interval
}

func (r *Runner) failCase(c *casefile.Case, msg, cmd string) {
	r.reporter.Emit(report.Event{
		Type:    report.EventAssertFail,
		Case:    c.Name,
		Source:  c.SourcePath,
		Message: msg,
		Cmd:     cmd,
	})
}

func (r *Runner) driveInputs(c *casefile.Case) error {
	for _, in := range c.Input {
		if err := r.doInput(in); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) doInput(in casefile.Input) error {
	if in.Path == "" {
		return nil
	}
	host := r.endpoint.AppHost
	if in.Host != "" {
		host = in.Host
	}
	if host == "" || r.endpoint.AppPort == 0 {
		return fmt.Errorf("input requires application endpoint; set --app-host/--app-port or provide fixture-derived app endpoint")
	}
	scheme := "http"
	if in.Scheme != "" {
		scheme = in.Scheme
	}
	method := http.MethodGet
	if in.Method != "" {
		method = strings.ToUpper(in.Method)
	}
	status := 200
	if in.Status != "" {
		parsed, err := strconv.Atoi(in.Status)
		if err != nil {
			return fmt.Errorf("input status %q is not an integer", in.Status)
		}
		status = parsed
	}
	headers := map[string]string{}
	if in.Headers != nil {
		maps.Copy(headers, in.Headers)
	} else {
		headers["Accept"] = "application/json"
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, host, r.endpoint.AppPort, in.Path)
	return requests.DoHTTPRequest(url, method, headers, in.Body, status)
}
