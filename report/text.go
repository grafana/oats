package report

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// write swallows write errors on purpose: a downstream pipe that died
// should not crash the runner, and the exit status still reflects the test
// outcome.
func (r *TextReporter) write(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, format, args...)
}

// TextReporter is the default Reporter. It emits compact text suitable for
// both humans and agents: silent on success at default verbosity, detailed
// per-failure blocks, and one summary line per run.
//
// When GITHUB_ACTIONS is set in the environment, failures are additionally
// emitted as ::error::file=...,line=...:: annotations so reviewers see them
// inline on the PR diff. The annotation channel is a thin pass-through over
// the same failure events that drive the human-readable blocks — no
// duplicate accounting, no separate sink.
type TextReporter struct {
	w          io.Writer
	v          Verbosity
	ghaEnabled bool
	runStart   time.Time
	pass       int
	fail       int
	skip       int
	failBlocks []string // buffered "FAIL ..." blocks, flushed at run.end
	knownErrAt map[string]struct{}
}

func NewTextReporter(w io.Writer, v Verbosity) *TextReporter {
	return &TextReporter{
		w:          w,
		v:          v,
		ghaEnabled: os.Getenv("GITHUB_ACTIONS") == "true",
		knownErrAt: make(map[string]struct{}),
	}
}

func (r *TextReporter) Emit(e Event) {
	switch e.Type {
	case EventRunStart:
		r.resetRunState()
		r.runStart = nonZeroOrNow(e.Ts)
	case EventRunEnd:
		r.flushRunEnd(e)
	case EventCasePass:
		r.pass++
		if r.v >= VerbosePasses {
			r.write("PASS %s\n", e.Case)
		}
	case EventCaseSkip:
		r.skip++
		if r.v >= VerbosePasses {
			r.write("SKIP %s\n", e.Case)
		}
	case EventAssertFail:
		r.recordFailure(e)
	case EventCaseFail:
		r.fail++
	case EventGCXExec:
		if r.v >= VerboseCmd {
			r.write("  $ %s\n", e.Cmd)
		}
	case EventFixtureStart, EventFixtureReady, EventFixtureTeardown:
		if r.v >= VerboseAll {
			r.write("[fixture %s] %s (%dms)\n", e.Fixture, e.Type, e.DurationMs)
		}
	}
}

func (r *TextReporter) Close() error { return nil }

func (r *TextReporter) resetRunState() {
	r.pass = 0
	r.fail = 0
	r.skip = 0
	r.failBlocks = nil
	r.knownErrAt = make(map[string]struct{})
}

func (r *TextReporter) recordFailure(e Event) {
	var b strings.Builder
	src := e.Source
	if src == "" {
		src = "(unknown source)"
	}
	fmt.Fprintf(&b, "FAIL %s  %s\n", e.Case, src)
	if e.Msg != "" {
		fmt.Fprintf(&b, "  %s\n", e.Msg)
	}
	if e.Cmd != "" {
		fmt.Fprintf(&b, "  $ %s\n", e.Cmd)
	}
	if e.StdoutExcerpt != "" {
		fmt.Fprintf(&b, "  stdout: %s\n", e.StdoutExcerpt)
	}
	r.failBlocks = append(r.failBlocks, b.String())

	if r.ghaEnabled {
		r.emitGHAAnnotation(e)
	}
}

func (r *TextReporter) emitGHAAnnotation(e Event) {
	if e.Source == "" {
		const key = "(unknown source):0"
		if _, dup := r.knownErrAt[key]; dup {
			return
		}
		r.knownErrAt[key] = struct{}{}

		msg := e.Msg
		if msg == "" {
			msg = "OATS assertion failed"
		}
		r.write("::error::%s\n", ghaEscape(msg))
		return
	}

	file, line := splitSource(e.Source)
	// Suppress duplicate annotations for the same source position so a case
	// with N substring failures does not flood the PR diff.
	key := fmt.Sprintf("%s:%d", file, line)
	if _, dup := r.knownErrAt[key]; dup {
		return
	}
	r.knownErrAt[key] = struct{}{}

	msg := e.Msg
	if msg == "" {
		msg = "OATS assertion failed"
	}
	msg = ghaEscape(msg)
	if line > 0 {
		r.write("::error file=%s,line=%d::%s\n", file, line, msg)
	} else {
		r.write("::error file=%s::%s\n", file, msg)
	}
}

func ghaEscape(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

func (r *TextReporter) flushRunEnd(e Event) {
	// Failure blocks first so the summary line is the final thing the reader
	// sees — useful for both humans (scroll to bottom) and CI logs (tail).
	for _, b := range r.failBlocks {
		r.write("\n")
		r.write("%s", b)
	}

	total := r.pass + r.fail + r.skip
	duration := durationOr(e, time.Since(r.runStart))
	switch {
	case r.fail == 0 && r.skip == 0:
		r.write("\nPASS %d/%d in %s\n", r.pass, total, duration)
	case r.fail == 0:
		r.write("\nPASS %d/%d (%d skipped) in %s\n", r.pass, total, r.skip, duration)
	default:
		r.write("\nFAIL %d/%d (%d failed, %d skipped) in %s\n",
			r.pass, total, r.fail, r.skip, duration)
	}
}

func nonZeroOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func durationOr(e Event, fallback time.Duration) time.Duration {
	if e.DurationMs > 0 {
		return time.Duration(e.DurationMs) * time.Millisecond
	}
	return fallback.Round(time.Millisecond)
}

// splitSource splits "path/to/file.yaml:42" into (file, line). When no line
// number is present (or the suffix is non-numeric), line is zero and file is
// the whole input. The contract for callers is "best effort": annotations
// fall back to file-only when a line number cannot be derived.
func splitSource(src string) (string, int) {
	if src == "" {
		return "", 0
	}
	idx := strings.LastIndex(src, ":")
	if idx < 0 {
		return src, 0
	}
	suffix := src[idx+1:]
	n := 0
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return src, 0
		}
		n = n*10 + int(c-'0')
	}
	return src[:idx], n
}
