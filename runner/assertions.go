// Signal assertion drivers: one runX method per signal type plus the shared
// evaluators that turn parsed rows and gcx output into assert.Failure lists.
package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/oats/assert"
	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/signalcmd"
	"github.com/grafana/oats/wait"
)

func (r *Runner) runTrace(ctx context.Context, c *casefile.Case, a *casefile.TraceAssertion) bool {
	if len(a.MatchSpans) > 0 {
		return r.runTraceStructured(ctx, c, a)
	}
	args := signalcmd.Traces(*a, 0)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		return evalCommonText(stdout, a.AssertionCommon)
	})
}

func (r *Runner) runTraceStructured(ctx context.Context, c *casefile.Case, a *casefile.TraceAssertion) bool {
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		searchArgs := signalcmd.Traces(*a, 0)
		searchCmd := signalcmd.Render(searchArgs)
		execCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		defer cancel()
		searchRes, err := r.exec.Execute(execCtx, searchArgs...)
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
		return evalTraceStructured(searchRes.Stdout, *a, rows, count, gcxParseHint(err, r.opts.GCXVersion))
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	cmdStr := signalcmd.Render(signalcmd.Traces(*a, 0))
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
		args := signalcmd.TraceGet(traceID, 0)
		execCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		res, err := r.exec.Execute(execCtx, args...)
		cancel()
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
	args := signalcmd.Logs(*a, 0)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, err := extractLogRows(stdout)
		return evalCommonStructured(stdout, a.AssertionCommon, rows, count, gcxParseHint(err, r.opts.GCXVersion))
	})
}

func (r *Runner) runMetric(ctx context.Context, c *casefile.Case, a *casefile.MetricAssertion) bool {
	args := signalcmd.Metrics(*a, 0)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if a.Value == "" && len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, actual, err := extractMetricRows(stdout)
		err = gcxParseHint(err, r.opts.GCXVersion)
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
	args := signalcmd.Profiles(*a, 0)
	return r.pollAssert(ctx, c, args, a.Absent, func(stdout, _ string, _ int) []assert.Failure {
		if len(a.Match) == 0 {
			return evalCommonText(stdout, a.AssertionCommon)
		}
		rows, count, err := extractProfileRows(stdout)
		return evalCommonStructured(stdout, a.AssertionCommon, rows, count, gcxParseHint(err, r.opts.GCXVersion))
	})
}

func gcxParseHint(err error, version string) error {
	if err == nil {
		return nil
	}
	var parseErr *gcxParseError
	if !errors.As(err, &parseErr) {
		return err
	}
	if version == "" {
		return fmt.Errorf("%w; if this started after a gcx upgrade, try --gcx-version with a known-compatible release or upgrade oats", err)
	}
	return fmt.Errorf("%w; selected GCX reports %q and may be using a newer unsupported response format, so try --gcx-version with a known-compatible release or upgrade oats", err, version)
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
