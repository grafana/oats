package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/v2case"
)

// stubExec is a deterministic Executor that returns the configured output
// regardless of args. It also records the args of every invocation so the
// test can assert on what gcx was asked to do.
type stubExec struct {
	stdout   string
	stderr   string
	exit     int
	err      error
	captured [][]string
}

func (s *stubExec) Execute(_ context.Context, args ...string) (*engine.Result, error) {
	s.captured = append(s.captured, args)
	if s.err != nil {
		return nil, s.err
	}
	return &engine.Result{
		Command:  append([]string{"gcx-stub"}, args...),
		Stdout:   s.stdout,
		Stderr:   s.stderr,
		ExitCode: s.exit,
	}, nil
}

func newRunner(t *testing.T, exec engine.Executor, opts Options) (*Runner, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerbosePasses)
	return New(exec, rep, Endpoint{GCXContext: "test"}, opts), &buf
}

func mustParse(t *testing.T, src string) *v2case.Case {
	t.Helper()
	c, err := v2case.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

const tracesCase = `
oats: 2
name: traces pass
seed:
  type: app
  compose: x.yml
expected:
  traces:
    - traceql: '{ resource.service.name = "svc" }'
      contains: ["svc"]
`

func TestRunCase_TracesPass(t *testing.T) {
	exec := &stubExec{stdout: "found span service.name=svc"}
	r, buf := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), mustParse(t, tracesCase))
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Errorf("RunCase: expected pass, got fail\n%s", buf.String())
	}
	if len(exec.captured) == 0 {
		t.Errorf("expected at least one gcx invocation")
	}
	cmd := exec.captured[0]
	if cmd[0] != "traces" || cmd[1] != "search" {
		t.Errorf("wrong verb chain: %v", cmd)
	}
	if !strings.Contains(buf.String(), "PASS traces pass") {
		t.Errorf("PASS line missing:\n%s", buf.String())
	}
}

const containsMissingCase = `
oats: 2
name: traces fail
seed:
  type: app
  compose: x.yml
expected:
  traces:
    - traceql: '{}'
      contains: ["needle"]
`

func TestRunCase_TracesFail_ContainsMissing(t *testing.T) {
	exec := &stubExec{stdout: "haystack without it"}
	r, buf := newRunner(t, exec, Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), mustParse(t, containsMissingCase))
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if ok {
		t.Errorf("RunCase: expected fail")
	}
	if !strings.Contains(buf.String(), "FAIL traces fail") {
		t.Errorf("FAIL header missing:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `substring "needle" not found`) {
		t.Errorf("specific failure message missing:\n%s", buf.String())
	}
}

const metricsValueCase = `
oats: 2
name: metric value
seed:
  type: app
  compose: x.yml
expected:
  metrics:
    - promql: 'rate(x[1m])'
      value: '>= 5'
`

func TestRunCase_MetricsValuePass(t *testing.T) {
	stdout := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"10"]}]}}`
	exec := &stubExec{stdout: stdout}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), mustParse(t, metricsValueCase))
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Errorf("expected metrics case to pass (value 10 >= 5)")
	}
}

func TestRunCase_MetricsValueFail(t *testing.T) {
	stdout := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"1"]}]}}`
	exec := &stubExec{stdout: stdout}
	r, buf := newRunner(t, exec, Options{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), mustParse(t, metricsValueCase))
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if ok {
		t.Errorf("expected metrics case to fail (value 1 < 5)")
	}
	if !strings.Contains(buf.String(), "expected value >= 5") {
		t.Errorf("value failure message missing:\n%s", buf.String())
	}
}

func TestRunCase_LogsStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"svc","trace_id":"abc123"},"values":[["1700000000","seed-log-line"]]}]}}`}
	r, buf := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
oats: 2
name: logs structured match
seed:
  type: app
  compose: x.yml
expected:
  logs:
    - logql: '{service_name="svc"}'
      match:
        - name: seed-log-line
          attributes:
            service_name: svc
            trace_id:
              present: true
`)

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Fatalf("expected structured log match to pass:\n%s", buf.String())
	}
	if !containsSequence(exec.captured[0], "-o", "json") {
		t.Fatalf("expected logs query to request json: %v", exec.captured[0])
	}
}

func TestRunCase_MetricsStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"svc"},"value":[1700000000,"1"]}]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
oats: 2
name: metrics structured match
seed:
  type: app
  compose: x.yml
expected:
  metrics:
    - promql: 'up{job="svc"}'
      match:
        - attributes:
            job: svc
`)

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Fatalf("expected structured metric match to pass")
	}
}

func TestRunCase_InlineOTLPSeedRequiresEndpoint(t *testing.T) {
	c := mustParse(t, `
oats: 2
name: inline seed
seed:
  type: inline-otlp
  traces:
    - service: svc
      spans:
        - name: op
expected:
  traces:
    - traceql: '{}'
      contains: ["svc"]
`)
	// Endpoint with no OTLPHTTP — seed step must fail loudly.
	exec := &stubExec{stdout: "svc"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test", OTLPHTTP: ""}, Options{SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})

	if ok {
		t.Errorf("expected fail when inline-otlp seed has no endpoint")
	}
	if !strings.Contains(buf.String(), "OTLPHTTP") {
		t.Errorf("seed-endpoint error not surfaced:\n%s", buf.String())
	}
}

func TestApproxRowCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a\nb\nc", 3},
		{"a\n\nb", 2},
		{"─\na\n═\n+\nb", 2},
	}
	for _, tc := range cases {
		got := approxRowCount(tc.in)
		if got != tc.want {
			t.Errorf("approxRowCount(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func containsSequence(haystack []string, needles ...string) bool {
	for i := 0; i+len(needles) <= len(haystack); i++ {
		match := true
		for j, needle := range needles {
			if haystack[i+j] != needle {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
