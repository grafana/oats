// Package runner orchestrates one case end-to-end: seed → poll-and-assert
// → report. It is intentionally agnostic about fixtures — the caller hands
// in already-resolved endpoints, so this layer ships before the
// fixture-lifecycle layer exists.
//
// One Runner instance handles one suite of cases sharing a fixture.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

func (r *Runner) runComposeLogCheck(ctx context.Context, c *casefile.Case, msg string) bool {
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		ok, err := searchComposeLogs(ctx, r.endpoint.CustomCheckEnv, msg)
		if err != nil {
			return []assert.Failure{{Rule: "compose-logs", Detail: err.Error()}}
		}
		if !ok {
			return []assert.Failure{{Rule: "compose-logs", Detail: fmt.Sprintf("missing %q", msg)}}
		}
		return nil
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:   report.EventAssertFail,
			Case:   c.Name,
			Source: c.SourcePath,
			Msg:    f.Error(),
			Cmd:    "compose-logs",
		})
	}
	return false
}

func (r *Runner) runCustomCheck(ctx context.Context, c *casefile.Case, chk *casefile.CustomCheck) bool {
	dir := "."
	if c.SourcePath != "" {
		dir = filepath.Dir(c.SourcePath)
	}
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		deadlineCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		defer cancel()

		cmd, cleanup, err := customCheckCommand(deadlineCtx, dir, chk.Script, r.endpoint.CustomCheckEnv)
		if err != nil {
			return []assert.Failure{{Rule: "custom-check-setup", Detail: err.Error()}}
		}
		defer cleanup()

		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return []assert.Failure{{Rule: "custom-check", Detail: err.Error() + "\n" + trimOutput(out.String())}}
		}
		return nil
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:   report.EventAssertFail,
			Case:   c.Name,
			Source: c.SourcePath,
			Msg:    f.Error(),
			Cmd:    "custom-check",
		})
	}
	return false
}

func customCheckCommand(ctx context.Context, dir, script string, extraEnv []string) (*exec.Cmd, func(), error) {
	cleanup := func() {}
	if strings.TrimSpace(script) == "" {
		return nil, cleanup, fmt.Errorf("empty script")
	}
	if looksLikeInlineScript(script) {
		f, err := os.CreateTemp("", "oats-custom-check-*.sh")
		if err != nil {
			return nil, cleanup, err
		}
		if _, err := f.WriteString(script); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		if err := os.Chmod(f.Name(), 0o700); err != nil {
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		cleanup = func() { _ = os.Remove(f.Name()) }
		cmd := exec.CommandContext(ctx, f.Name())
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), extraEnv...)
		return cmd, cleanup, nil
	}
	cmd := exec.CommandContext(ctx, resolveCustomCheckPath(dir, script))
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), extraEnv...)
	return cmd, cleanup, nil
}

func searchComposeLogs(ctx context.Context, extraEnv []string, needle string) (bool, error) {
	if envValue(extraEnv, "OATS_FIXTURE_TYPE") != "compose" {
		return false, fmt.Errorf("compose-logs requires a compose fixture")
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--no-color")
	cmd.Env = append(cmd.Environ(), extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker compose logs: %w\n%s", err, trimOutput(out.String()))
	}
	return strings.Contains(out.String(), needle), nil
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

func looksLikeInlineScript(script string) bool {
	trimmed := strings.TrimSpace(script)
	return strings.Contains(trimmed, "\n") || strings.HasPrefix(trimmed, "#!")
}

func resolveCustomCheckPath(dir, script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	if filepath.IsAbs(script) {
		return script
	}
	if strings.ContainsRune(script, os.PathSeparator) {
		joined := filepath.Clean(filepath.Join(dir, script))
		if abs, err := filepath.Abs(joined); err == nil {
			return abs
		}
		return joined
	}
	return script
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}

func (r *Runner) seedCase(c *casefile.Case) error {
	switch c.Seed.Type {
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
		return r.seeder.Send(payload)
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
		res, err := r.exec.Execute(ctx, args...)
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

func (r *Runner) caseInterval(c *casefile.Case) time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return r.opts.Interval
}

func (r *Runner) runTrace(ctx context.Context, c *casefile.Case, a *casefile.TraceAssertion) bool {
	if len(a.MatchSpans) > 0 {
		return r.runTraceStructured(ctx, c, a)
	}
	args := signalcmd.Traces(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		return evalCommonText(stdout, a.AssertionCommon)
	})
}

func (r *Runner) runTraceStructured(ctx context.Context, c *casefile.Case, a *casefile.TraceAssertion) bool {
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		searchArgs := signalcmd.Traces(*a, r.opts.Timeout)
		searchCmd := signalcmd.Render(searchArgs)
		searchRes, err := r.exec.Execute(ctx, searchArgs...)
		if err != nil {
			return []assert.Failure{{Rule: "exec", Detail: err.Error()}}
		}
		r.reporter.Emit(report.Event{Type: report.EventGCXExec, Case: c.Name, Cmd: searchCmd})
		if searchRes.ExitCode != 0 {
			detail := strings.TrimSpace(searchRes.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("gcx exit code %d", searchRes.ExitCode)
			}
			return []assert.Failure{{Rule: "exec", Detail: detail}}
		}
		rows, count, err := r.fetchTraceRows(ctx, c, searchRes.Stdout)
		return evalTraceStructured(searchRes.Stdout, *a, rows, count, err)
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	cmdStr := signalcmd.Render(signalcmd.Traces(*a, r.opts.Timeout))
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

func (r *Runner) fetchTraceRows(ctx context.Context, c *casefile.Case, searchStdout string) ([]assert.Row, int, error) {
	traceIDs, count, err := extractTraceIDs(searchStdout)
	if err != nil {
		return nil, 0, err
	}
	if len(traceIDs) == 0 {
		rows, parsedCount, err := extractTraceRows(searchStdout)
		if err != nil {
			return nil, count, err
		}
		if count == 0 {
			count = parsedCount
		}
		return rows, count, nil
	}
	var rows []assert.Row
	for _, traceID := range traceIDs {
		args := signalcmd.TraceGet(traceID, r.opts.Timeout)
		res, err := r.exec.Execute(ctx, args...)
		if err != nil {
			return nil, count, fmt.Errorf("trace %s fetch: %w", traceID, err)
		}
		r.reporter.Emit(report.Event{Type: report.EventGCXExec, Case: c.Name, Cmd: signalcmd.Render(args)})
		if res.ExitCode != 0 {
			detail := strings.TrimSpace(res.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("gcx exit code %d", res.ExitCode)
			}
			return nil, count, fmt.Errorf("trace %s fetch: %s", traceID, detail)
		}
		traceRows, _, err := extractTraceRows(res.Stdout)
		if err != nil {
			return nil, count, err
		}
		rows = append(rows, traceRows...)
	}
	return rows, count, nil
}

func (r *Runner) runLog(ctx context.Context, c *casefile.Case, a *casefile.LogAssertion) bool {
	args := signalcmd.Logs(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, err := extractLogRows(stdout)
		return evalCommonStructured(stdout, a.AssertionCommon, rows, count, err)
	})
}

func (r *Runner) runMetric(ctx context.Context, c *casefile.Case, a *casefile.MetricAssertion) bool {
	args := signalcmd.Metrics(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if a.Value == "" && len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, actual, err := extractMetricRows(stdout)
		fails := evalCommonStructured(stdout, a.AssertionCommon, rows, count, err)
		if a.Value != "" {
			if err != nil {
				fails = append(fails, assert.Failure{Rule: "value", Detail: err.Error()})
			} else {
				fails = append(fails, assert.Value(actual, a.Value)...)
			}
		}
		return fails
	})
}

func (r *Runner) runProfile(ctx context.Context, c *casefile.Case, a *casefile.ProfileAssertion) bool {
	args := signalcmd.Profiles(*a, r.opts.Timeout)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, err := extractProfileRows(stdout)
		return evalCommonStructured(stdout, a.AssertionCommon, rows, count, err)
	})
}

// evalCommonText runs the assertions that every signal type shares when gcx
// output is plain text rather than JSON.
func evalCommonText(stdout string, c casefile.AssertionCommon) []assert.Failure {
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

func evalTraceStructured(stdout string, a casefile.TraceAssertion, rows []assert.Row, count int, parseErr error) []assert.Failure {
	var fails []assert.Failure
	fails = append(fails, assert.Contains(stdout, a.Contains)...)
	fails = append(fails, assert.NotContains(stdout, a.NotContains)...)
	fails = append(fails, assert.Regex(stdout, a.Regex)...)
	if parseErr != nil {
		fails = append(fails, assert.Failure{Rule: "match_spans", Detail: parseErr.Error()})
		return fails
	}
	if len(a.MatchSpans) > 0 {
		spanFails := assert.MatchRows(rows, a.MatchSpans)
		for i := range spanFails {
			spanFails[i].Rule = "match_spans"
		}
		fails = append(fails, spanFails...)
	}
	if a.Count != "" {
		fails = append(fails, assert.Count(count, a.Count)...)
	}
	if a.Absent {
		fails = append(fails, assert.Absent(count)...)
	}
	return fails
}

func evalCommonStructured(stdout string, c casefile.AssertionCommon, rows []assert.Row, count int, parseErr error) []assert.Failure {
	var fails []assert.Failure
	fails = append(fails, assert.Contains(stdout, c.Contains)...)
	fails = append(fails, assert.NotContains(stdout, c.NotContains)...)
	fails = append(fails, assert.Regex(stdout, c.Regex)...)
	if parseErr != nil {
		fails = append(fails, assert.Failure{Rule: "match", Detail: parseErr.Error()})
		return fails
	}
	if len(c.Match) > 0 {
		fails = append(fails, assert.MatchRows(rows, c.Match)...)
	}
	if c.Count != "" {
		fails = append(fails, assert.Count(count, c.Count)...)
	}
	if c.Absent {
		fails = append(fails, assert.Absent(count)...)
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
		if strings.HasPrefix(t, "hint: use --json") {
			continue
		}
		if t == "No data" {
			continue
		}
		// Skip lines that look like table-headers / dividers in gcx text mode.
		if strings.HasPrefix(t, "─") || strings.HasPrefix(t, "═") || strings.HasPrefix(t, "+") || looksLikeGCXHeader(t) {
			continue
		}
		n++
	}
	return n
}

func looksLikeGCXHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	for _, f := range fields {
		hasLetter := false
		for _, r := range f {
			switch {
			case r >= 'A' && r <= 'Z':
				hasLetter = true
			case r >= '0' && r <= '9':
			case r == '_' || r == '.' || r == '/':
			default:
				return false
			}
		}
		if !hasLetter {
			return false
		}
	}
	return true
}

// extractMetricValue parses the first numeric data point out of `gcx metrics
// query -o json` output. The schema follows gcx's JSON shape; we only look
// at the fields we need so additions don't break us.
func extractMetricRows(stdout string) ([]assert.Row, int, float64, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, 0, fmt.Errorf("metric value parse: empty result")
	}
	var generic struct {
		Data struct {
			Result []struct {
				Metric map[string]any `json:"metric"`
				Value  [2]any         `json:"value"`
				Values [][2]any       `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		return nil, 0, 0, fmt.Errorf("metric JSON parse: %w", err)
	}
	if len(generic.Data.Result) == 0 {
		return nil, 0, 0, fmt.Errorf("metric value parse: empty result")
	}
	rows := make([]assert.Row, 0, len(generic.Data.Result))
	for _, item := range generic.Data.Result {
		attrs := stringifyMap(item.Metric)
		rows = append(rows, assert.Row{
			Name:       attrs["__name__"],
			Attributes: attrs,
		})
	}
	r := generic.Data.Result[0]
	raw, ok := r.Value[1].(string)
	if !ok && len(r.Values) > 0 {
		raw, ok = r.Values[len(r.Values)-1][1].(string)
	}
	if !ok {
		return rows, len(generic.Data.Result), 0, fmt.Errorf("metric value parse: result point has no scalar value")
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return rows, len(generic.Data.Result), 0, fmt.Errorf("metric value parse: %q is not a number", raw)
	}
	return rows, len(generic.Data.Result), f, nil
}

func (r *Runner) failCase(c *casefile.Case, msg, cmd string) {
	r.reporter.Emit(report.Event{
		Type:   report.EventAssertFail,
		Case:   c.Name,
		Source: c.SourcePath,
		Msg:    msg,
		Cmd:    cmd,
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

func extractLogRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var generic struct {
		Data struct {
			Result []struct {
				Stream map[string]any `json:"stream"`
				Values [][]any        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		return nil, 0, fmt.Errorf("log JSON parse: %w", err)
	}
	var rows []assert.Row
	for _, stream := range generic.Data.Result {
		attrs := stringifyMap(stream.Stream)
		for _, pair := range stream.Values {
			body := ""
			if len(pair) > 1 {
				body = fmt.Sprint(pair[1])
			}
			rows = append(rows, assert.Row{Name: body, Attributes: attrs})
		}
	}
	return rows, len(rows), nil
}

func extractTraceRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, 0, fmt.Errorf("trace JSON parse: %w", err)
	}
	if rows, ok := extractOTLPTraceRows(root); ok {
		return rows, len(rows), nil
	}
	count := traceResultCount(root)
	rows := collectNamedRows(root)
	if count == 0 {
		count = len(rows)
	}
	return rows, count, nil
}

func extractTraceIDs(stdout string) ([]string, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var payload struct {
		Traces []struct {
			TraceID string `json:"traceID"`
		} `json:"traces"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, 0, fmt.Errorf("trace JSON parse: %w", err)
	}
	ids := make([]string, 0, len(payload.Traces))
	for _, tr := range payload.Traces {
		if tr.TraceID != "" {
			ids = append(ids, tr.TraceID)
		}
	}
	return ids, len(payload.Traces), nil
}

func extractProfileRows(stdout string) ([]assert.Row, int, error) {
	if strings.TrimSpace(stdout) == "" {
		return nil, 0, nil
	}
	var root any
	if err := json.Unmarshal([]byte(stdout), &root); err != nil {
		return nil, 0, fmt.Errorf("profile JSON parse: %w", err)
	}
	if names := flamebearerNames(root); len(names) > 0 {
		rows := make([]assert.Row, 0, len(names))
		for _, name := range names {
			rows = append(rows, assert.Row{Name: name, Attributes: map[string]string{}})
		}
		return rows, len(rows), nil
	}
	if names := flamegraphNames(root); len(names) > 0 {
		rows := make([]assert.Row, 0, len(names))
		for _, name := range names {
			rows = append(rows, assert.Row{Name: name, Attributes: map[string]string{}})
		}
		return rows, len(rows), nil
	}
	rows := collectNamedRows(root)
	return rows, len(rows), nil
}

func traceResultCount(root any) int {
	m, ok := root.(map[string]any)
	if !ok {
		return 0
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		return 0
	}
	result, ok := data["result"].([]any)
	if !ok {
		return 0
	}
	return len(result)
}

func collectNamedRows(v any) []assert.Row {
	var rows []assert.Row
	var walk func(any)
	walk = func(cur any) {
		switch t := cur.(type) {
		case map[string]any:
			if row, ok := maybeRow(t); ok {
				rows = append(rows, row)
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return rows
}

func maybeRow(m map[string]any) (assert.Row, bool) {
	row := assert.Row{Attributes: map[string]string{}}
	for _, key := range []string{"name", "spanName", "span_name", "body", "rootTraceName"} {
		if v, ok := m[key]; ok {
			row.Name = fmt.Sprint(v)
			break
		}
	}
	for _, key := range []string{"attributes", "metric", "stream", "resourceAttributes", "resource_attributes"} {
		if child, ok := m[key]; ok {
			for k, v := range stringifyMapAny(child) {
				row.Attributes[k] = v
			}
		}
	}
	if resource, ok := m["resource"].(map[string]any); ok {
		if attrs, ok := resource["attributes"]; ok {
			for k, v := range stringifyMapAny(attrs) {
				row.Attributes[k] = v
			}
		}
	}
	if v, ok := m["rootServiceName"]; ok {
		row.Attributes["service.name"] = fmt.Sprint(v)
	}
	if v, ok := m["traceID"]; ok {
		row.Attributes["trace_id"] = fmt.Sprint(v)
	}
	if row.Name == "" && len(row.Attributes) == 0 {
		return assert.Row{}, false
	}
	return row, true
}

func stringifyMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func stringifyMapAny(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	return stringifyMap(m)
}

func extractOTLPTraceRows(root any) ([]assert.Row, bool) {
	top, ok := root.(map[string]any)
	if !ok {
		return nil, false
	}
	if trace, ok := top["trace"].(map[string]any); ok {
		top = trace
	}
	resourceSpans, ok := top["resourceSpans"].([]any)
	if !ok {
		resourceSpans, ok = top["batches"].([]any)
	}
	if !ok {
		// Some wrappers may nest under "data" first.
		if data, ok := top["data"].(map[string]any); ok {
			if nested, ok := data["resourceSpans"].([]any); ok {
				resourceSpans = nested
			} else if nested, ok := data["batches"].([]any); ok {
				resourceSpans = nested
			}
		}
		if !ok {
			return nil, false
		}
	}
	var rows []assert.Row
	for _, rsAny := range resourceSpans {
		rs, ok := rsAny.(map[string]any)
		if !ok {
			continue
		}
		resourceAttrs := map[string]string{}
		if resource, ok := rs["resource"].(map[string]any); ok {
			resourceAttrs = parseOTelAttributeList(resource["attributes"])
		}
		scopeSpans, _ := rs["scopeSpans"].([]any)
		for _, ssAny := range scopeSpans {
			ss, ok := ssAny.(map[string]any)
			if !ok {
				continue
			}
			scopeName := ""
			if scope, ok := ss["scope"].(map[string]any); ok {
				scopeName = fmt.Sprint(scope["name"])
			}
			spans, _ := ss["spans"].([]any)
			for _, spAny := range spans {
				sp, ok := spAny.(map[string]any)
				if !ok {
					continue
				}
				attrs := map[string]string{}
				for k, v := range resourceAttrs {
					attrs[k] = v
				}
				for k, v := range parseOTelAttributeList(sp["attributes"]) {
					attrs[k] = v
				}
				if scopeName != "" {
					attrs["otel.scope.name"] = scopeName
					attrs["otel.library.name"] = scopeName
				}
				if kind, ok := sp["kind"]; ok {
					attrs["kind"] = fmt.Sprint(kind)
				}
				rows = append(rows, assert.Row{
					Name:       fmt.Sprint(sp["name"]),
					Attributes: attrs,
				})
			}
		}
	}
	if len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func parseOTelAttributeList(v any) map[string]string {
	list, ok := v.([]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(list))
	for _, itemAny := range list {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprint(item["key"])
		if key == "" {
			continue
		}
		value, _ := item["value"].(map[string]any)
		out[key] = parseOTelAnyValue(value)
	}
	return out
}

func parseOTelAnyValue(m map[string]any) string {
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue"} {
		if v, ok := m[key]; ok {
			return fmt.Sprint(v)
		}
	}
	if arr, ok := m["arrayValue"].(map[string]any); ok {
		if vals, ok := arr["values"].([]any); ok {
			parts := make([]string, 0, len(vals))
			for _, val := range vals {
				if child, ok := val.(map[string]any); ok {
					parts = append(parts, parseOTelAnyValue(child))
				}
			}
			return strings.Join(parts, ",")
		}
	}
	return ""
}

func flamebearerNames(root any) []string {
	top, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	flamebearer, ok := top["flamebearer"].(map[string]any)
	if !ok {
		if data, ok := top["data"].(map[string]any); ok {
			flamebearer, _ = data["flamebearer"].(map[string]any)
		}
	}
	if flamebearer == nil {
		return nil
	}
	raw, ok := flamebearer["names"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func flamegraphNames(root any) []string {
	top, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	flamegraph, ok := top["flamegraph"].(map[string]any)
	if !ok {
		if data, ok := top["data"].(map[string]any); ok {
			flamegraph, _ = data["flamegraph"].(map[string]any)
		}
	}
	if flamegraph == nil {
		return nil
	}
	raw, ok := flamegraph["names"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := fmt.Sprint(item)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
