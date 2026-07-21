package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/assert"
	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
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

func TestNew_PropagatesOatsVersionToSeeder(t *testing.T) {
	r, _ := newRunner(t, &stubExec{}, Options{OatsVersion: "0.7.0"})
	if got := r.seeder.Version; got != "0.7.0" {
		t.Fatalf("seed version = %q, want 0.7.0", got)
	}
}

func mustParse(t *testing.T, src string) *casefile.Case {
	t.Helper()
	c, err := casefile.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

const tracesCase = `
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

func TestRunCase_DrivesInputRequests(t *testing.T) {
	var hits int
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPost || r.URL.Path != "/rolldice" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer app.Close()
	host, port := splitHostPort(t, app.Listener.Addr().String())

	exec := &stubExec{stdout: "found span service.name=svc"}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})
	r.endpoint.AppHost = host
	r.endpoint.AppPort = port

	c := mustParse(t, `
name: traces pass with input
seed:
  type: app
  compose: x.yml
input:
  - path: /rolldice
    method: POST
    status: "201"
expected:
  traces:
    - traceql: '{ resource.service.name = "svc" }'
      contains: ["svc"]
`)

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("expected case to pass")
	}
	if hits == 0 {
		t.Fatalf("expected at least one input request")
	}
}

func TestRunCase_InlineOTLPSeedRequiresEndpoint(t *testing.T) {
	c := mustParse(t, `
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

func TestRunCase_CustomCheckScriptPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "verify.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := mustParse(t, `
name: custom check path
seed:
  type: app
expected:
  custom-checks:
    - script: ./verify.sh
`)
	c.SourcePath = filepath.Join(dir, "case.yaml")

	exec := &stubExec{}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test"}, Options{Timeout: 200 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("expected custom check path case to pass:\n%s", buf.String())
	}
}

func TestRunCase_CustomCheckInlineScript(t *testing.T) {
	c := mustParse(t, `
name: custom check inline
seed:
  type: app
expected:
  custom-checks:
    - script: |
        #!/bin/sh
        exit 0
`)

	exec := &stubExec{}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test"}, Options{Timeout: 200 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("expected inline custom check case to pass:\n%s", buf.String())
	}
}

func TestRunCase_CustomCheckFailureSurfaced(t *testing.T) {
	c := mustParse(t, `
name: custom check fail
seed:
  type: app
expected:
  custom-checks:
    - script: |
        #!/bin/sh
        echo bad
        exit 1
`)

	exec := &stubExec{}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test"}, Options{Timeout: 200 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if ok {
		t.Fatalf("expected inline custom check case to fail")
	}
	if !strings.Contains(buf.String(), "custom-check: exit status 1") || !strings.Contains(buf.String(), "bad") {
		t.Fatalf("expected custom-check failure output, got:\n%s", buf.String())
	}
}

func TestRunCase_ComposeLogsPass(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	docker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\necho 'service boot complete'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := mustParse(t, `
name: compose logs pass
seed:
  type: app
expected:
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      contains: seed-log-line
  compose-logs:
    - service boot complete
`)

	exec := &stubExec{stdout: "seed-log-line"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{
		GCXContext: "test",
		CustomCheckEnv: []string{
			"OATS_FIXTURE_TYPE=compose",
			"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
	}, Options{Timeout: 200 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("expected compose-logs case to pass:\n%s", buf.String())
	}
}

func TestSearchComposeLogs_UsesPodman(t *testing.T) {
	dir := t.TempDir()
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte("#!/bin/sh\ncase \"$*\" in\n  'compose logs') echo 'podman service started' ;;\n  *) echo \"unexpected args: $*\" >&2; exit 1 ;;\nesac\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ok, err := searchComposeLogs(context.Background(), []string{
		"OATS_FIXTURE_TYPE=compose",
		"OATS_CONTAINER_RUNTIME=podman",
	}, "podman service started")
	if err != nil {
		t.Fatalf("searchComposeLogs: %v", err)
	}
	if !ok {
		t.Fatal("searchComposeLogs returned false")
	}
}

func TestRunCase_ComposeLogsMissingSurfaced(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	docker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\necho 'different output'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := mustParse(t, `
name: compose logs missing
seed:
  type: app
expected:
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      contains: seed-log-line
  compose-logs:
    - expected line
`)

	exec := &stubExec{stdout: "seed-log-line"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{
		GCXContext: "test",
		CustomCheckEnv: []string{
			"OATS_FIXTURE_TYPE=compose",
			"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
	}, Options{Timeout: 200 * time.Millisecond, Interval: 20 * time.Millisecond, SeedSettleDelay: 1})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if ok {
		t.Fatalf("expected compose-logs case to fail")
	}
	if !strings.Contains(buf.String(), `compose-logs: missing "expected line"`) {
		t.Fatalf("expected compose-logs failure output, got:\n%s", buf.String())
	}
}

func TestRunCase_ComposeLogsRequireComposeFixture(t *testing.T) {
	c := mustParse(t, `
name: compose logs wrong fixture
seed:
  type: app
expected:
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      contains: seed-log-line
  compose-logs:
    - expected line
`)

	exec := &stubExec{stdout: "seed-log-line"}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	r := New(exec, rep, Endpoint{GCXContext: "test", CustomCheckEnv: []string{"OATS_FIXTURE_TYPE=remote"}}, Options{
		Timeout:         200 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 1,
	})

	r.reporter.Emit(report.Event{Type: report.EventRunStart})
	ok := r.RunCase(context.Background(), c)
	r.reporter.Emit(report.Event{Type: report.EventRunEnd})
	if ok {
		t.Fatalf("expected compose-logs case to fail")
	}
	if !strings.Contains(buf.String(), "compose-logs requires a compose fixture") {
		t.Fatalf("expected compose fixture error, got:\n%s", buf.String())
	}
}

func TestResolveCustomCheckPath(t *testing.T) {
	dir := "/tmp/cases"
	cases := []struct {
		name   string
		script string
		want   string
	}{
		{name: "bare command stays bare", script: "verify", want: "verify"},
		{name: "relative path resolved against case dir", script: "./scripts/verify.sh", want: filepath.Clean("/tmp/cases/scripts/verify.sh")},
		{name: "parent path resolved against case dir", script: "../verify.sh", want: filepath.Clean("/tmp/verify.sh")},
		{name: "absolute path preserved", script: "/opt/check.sh", want: "/opt/check.sh"},
	}
	for _, tc := range cases {
		if got := resolveCustomCheckPath(dir, tc.script); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
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
		{"TRACE_ID  SERVICE  NAME  DURATION  START", 0},
		{"TIME  STREAM  MESSAGE", 0},
		{"TRACE_ID  SERVICE  NAME  DURATION  START\nabc svc GET /x 1ms now", 1},
	}
	for _, tc := range cases {
		got := approxRowCount(tc.in)
		if got != tc.want {
			t.Errorf("approxRowCount(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestExtractTraceRows_OTLPShape(t *testing.T) {
	path := filepath.Join("..", "testhelpers", "tempo", "responses", "testdata", "trace_by_id.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rows, count, parseErr := extractTraceRows(string(data))
	if parseErr != nil {
		t.Fatalf("extractTraceRows: %v", parseErr)
	}
	if count == 0 || len(rows) == 0 {
		t.Fatalf("expected OTLP trace rows, got count=%d rows=%d", count, len(rows))
	}
	found := false
	for _, row := range rows {
		if row.Name == "GET /stock" && row.Attributes["http.route"] == "/stock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected extracted span row for GET /stock with http.route=/stock")
	}
}

func TestExtractProfileRows_FlamebearerShape(t *testing.T) {
	rows, count, err := extractProfileRows(`{"flamebearer":{"names":["main","worker"]}}`)
	if err != nil {
		t.Fatalf("extractProfileRows: %v", err)
	}
	if count != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 rows, got count=%d rows=%d", count, len(rows))
	}
	if rows[0].Name != "main" || rows[1].Name != "worker" {
		t.Fatalf("unexpected rows: %+v", rows)
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

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}
	return host, port
}

func TestExtractMetricRowsBranches(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantErr string
		wantVal float64
		wantN   int
	}{
		{
			name:    "empty",
			wantErr: "empty result",
		},
		{
			name:    "malformed JSON",
			stdout:  "not json",
			wantErr: "metric JSON parse",
		},
		{
			name:    "empty vector",
			stdout:  `{"data":{"result":[]}}`,
			wantErr: "empty result",
		},
		{
			name:    "vector",
			stdout:  `{"data":{"result":[{"metric":{"__name__":"up","job":"oats"},"value":[1,"2.5"]}]}}`,
			wantVal: 2.5,
			wantN:   1,
		},
		{
			name:    "matrix fallback",
			stdout:  `{"data":{"result":[{"metric":{},"values":[[1,"3"]]}]}}`,
			wantVal: 3,
			wantN:   1,
		},
		{
			name:    "missing scalar",
			stdout:  `{"data":{"result":[{"metric":{}}]}}`,
			wantErr: "no scalar value",
			wantN:   1,
		},
		{
			name:    "invalid scalar",
			stdout:  `{"data":{"result":[{"metric":{},"value":[1,"wat"]}]}}`,
			wantErr: "is not a number",
			wantN:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, count, value, err := extractMetricRows(tt.stdout)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("extractMetricRows: %v", err)
			}
			if count != tt.wantN {
				t.Errorf("count = %d, want %d", count, tt.wantN)
			}
			if tt.wantErr == "" && value != tt.wantVal {
				t.Errorf("value = %v, want %v", value, tt.wantVal)
			}
			if tt.wantN > 0 && len(rows) != tt.wantN {
				t.Errorf("rows = %d, want %d", len(rows), tt.wantN)
			}
		})
	}
}

func TestExtractLogRowsBranches(t *testing.T) {
	rows, count, err := extractLogRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty logs = %#v, %d, %v", rows, count, err)
	}

	rows, count, err = extractLogRows(`{"data":{"result":[{"stream":{"service":"api"},"values":[["1","ready"],["2"]]}]}}`)
	if err != nil {
		t.Fatalf("extractLogRows: %v", err)
	}
	if count != 2 || rows[0].Name != "ready" || rows[1].Name != "" || rows[0].Attributes["service"] != "api" {
		t.Fatalf("unexpected log rows: %#v count=%d", rows, count)
	}
	if _, _, err := extractLogRows("not json"); err == nil || !strings.Contains(err.Error(), "log JSON parse") {
		t.Fatalf("malformed log error = %v", err)
	}
}

func TestExtractTraceRowsFallbacks(t *testing.T) {
	rows, count, err := extractTraceRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty traces = %#v, %d, %v", rows, count, err)
	}

	stdout := `{"data":{"result":[{"spanName":"operation","attributes":{"http.method":"GET"},"resource":{"attributes":{"service.name":"api"}},"rootServiceName":"api","traceID":"abc"}]}}`
	rows, count, err = extractTraceRows(stdout)
	if err != nil {
		t.Fatalf("extractTraceRows: %v", err)
	}
	if count != 1 || len(rows) == 0 {
		t.Fatalf("fallback rows = %#v count=%d", rows, count)
	}
	if rows[0].Name != "operation" || rows[0].Attributes["service.name"] != "api" || rows[0].Attributes["trace_id"] != "abc" {
		t.Fatalf("unexpected fallback row: %#v", rows[0])
	}
	if _, _, err := extractTraceRows("not json"); err == nil || !strings.Contains(err.Error(), "trace JSON parse") {
		t.Fatalf("malformed trace error = %v", err)
	}
}

func TestExtractTraceIDs(t *testing.T) {
	ids, count, err := extractTraceIDs("")
	if err != nil || ids != nil || count != 0 {
		t.Fatalf("empty trace IDs = %#v, %d, %v", ids, count, err)
	}
	ids, count, err = extractTraceIDs(`{"traces":[{"traceID":"a"},{"traceID":""},{"traceID":"b"}]}`)
	if err != nil || strings.Join(ids, ",") != "a,b" || count != 3 {
		t.Fatalf("trace IDs = %#v, %d, %v", ids, count, err)
	}
	if _, _, err := extractTraceIDs("not json"); err == nil || !strings.Contains(err.Error(), "trace JSON parse") {
		t.Fatalf("malformed trace IDs error = %v", err)
	}
}

func TestExtractProfileRowsBranches(t *testing.T) {
	for name, input := range map[string]string{
		"direct flamegraph": `{"flamegraph":{"names":["root","worker",""]}}`,
		"nested flamegraph": `{"data":{"flamegraph":{"names":["root"]}}}`,
		"named rows":        `{"rows":[{"name":"profile-row"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rows, count, err := extractProfileRows(input)
			if err != nil || count != len(rows) || count == 0 {
				t.Fatalf("rows = %#v count=%d err=%v", rows, count, err)
			}
		})
	}
	rows, count, err := extractProfileRows("")
	if err != nil || rows != nil || count != 0 {
		t.Fatalf("empty profiles = %#v, %d, %v", rows, count, err)
	}
	if _, _, err := extractProfileRows("not json"); err == nil || !strings.Contains(err.Error(), "profile JSON parse") {
		t.Fatalf("malformed profile error = %v", err)
	}
}

func TestParseOTelHelpers(t *testing.T) {
	root := map[string]any{
		"trace": map[string]any{
			"batches": []any{
				map[string]any{
					"resource": map[string]any{"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "api"}},
					}},
					"scopeSpans": []any{map[string]any{
						"scope": map[string]any{"name": "instrumentation"},
						"spans": []any{map[string]any{
							"name": "operation",
							"kind": 2,
							"attributes": []any{
								map[string]any{"key": "count", "value": map[string]any{"intValue": 3}},
								map[string]any{"key": "flags", "value": map[string]any{"arrayValue": map[string]any{"values": []any{
									map[string]any{"boolValue": true},
									map[string]any{"doubleValue": 1.5},
								}}}},
							},
						}},
					}},
				},
			},
		},
	}
	rows, ok := extractOTLPTraceRows(root)
	if !ok || len(rows) != 1 {
		t.Fatalf("extractOTLPTraceRows = %#v, %v", rows, ok)
	}
	if rows[0].Attributes["service.name"] != "api" || rows[0].Attributes["count"] != "3" || rows[0].Attributes["flags"] != "true,1.5" || rows[0].Attributes["kind"] != "2" {
		t.Fatalf("OTLP attributes = %#v", rows[0].Attributes)
	}

	if got := stringifyMapAny("not a map"); len(got) != 0 {
		t.Fatalf("stringifyMapAny non-map = %#v", got)
	}
	if rows, ok := extractOTLPTraceRows(map[string]any{"resourceSpans": []any{}}); ok || rows != nil {
		t.Fatalf("empty OTLP rows = %#v, %v", rows, ok)
	}
}

func TestToSeedPayload(t *testing.T) {
	payload, err := toSeedPayload(casefile.Seed{
		Traces:  []casefile.SeedTrace{{Service: "api", Spans: []casefile.SeedSpan{{Name: "op", Duration: "2ms"}}}},
		Logs:    []casefile.SeedLog{{Service: "api", Body: "ready", SeverityNumber: 9, SeverityText: "INFO"}},
		Metrics: []casefile.SeedMetric{{Service: "api", Name: "requests", Value: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Traces) != 1 || payload.Traces[0].Span.Duration.String() != "2ms" || len(payload.Logs) != 1 || len(payload.Metrics) != 1 {
		t.Fatalf("unexpected seed payload: %+v", payload)
	}
	if _, err := toSeedPayload(casefile.Seed{Traces: []casefile.SeedTrace{{Spans: []casefile.SeedSpan{{Name: "op", Duration: "bad"}}}}}); err == nil || !strings.Contains(err.Error(), "invalid duration") {
		t.Fatalf("invalid duration error = %v", err)
	}
}

func TestRunCase_TraceStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"data":{"result":[{"spanName":"operation","attributes":{"service.name":"svc"}}]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
name: structured trace
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
      match_spans:
        - name: operation
          attributes:
            - key: service.name
              value: svc
`)
	if !r.RunCase(context.Background(), c) {
		t.Fatal("expected structured trace case to pass")
	}
}

func TestRunCase_ProfileStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"flamebearer":{"names":["main","worker"]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
name: structured profile
seed:
  type: app
expected:
  profiles:
    - query: process_cpu
      match:
        - name: worker
`)
	if !r.RunCase(context.Background(), c) {
		t.Fatal("expected structured profile case to pass")
	}
}

func TestFetchTraceRowsByID(t *testing.T) {
	exec := &stubExec{stdout: `{"data":{"result":[{"spanName":"operation"}]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond})
	c := mustParse(t, `
name: trace lookup
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
`)

	rows, count, err := r.fetchTraceRows(context.Background(), c, `{"traces":[{"traceID":"abc"}]}`)
	if err != nil {
		t.Fatalf("fetchTraceRows: %v", err)
	}
	if count != 1 || len(rows) != 1 || rows[0].Name != "operation" {
		t.Fatalf("rows=%#v count=%d", rows, count)
	}
	if len(exec.captured) != 1 || exec.captured[0][0] != "traces" || exec.captured[0][1] != "get" || exec.captured[0][len(exec.captured[0])-1] != "abc" {
		t.Fatalf("unexpected TraceGet args: %#v", exec.captured)
	}
}

func TestDoInputValidation(t *testing.T) {
	r, _ := newRunner(t, &stubExec{}, Options{Timeout: time.Millisecond})
	if err := r.doInput(casefile.Input{Path: "/health"}); err == nil || !strings.Contains(err.Error(), "application endpoint") {
		t.Fatalf("missing endpoint error = %v", err)
	}
	if err := r.doInput(casefile.Input{}); err != nil {
		t.Fatalf("empty input should be ignored: %v", err)
	}

	invalidStatus, _ := newRunner(t, &stubExec{}, Options{Timeout: time.Millisecond})
	invalidStatus.endpoint.AppHost = "127.0.0.1"
	invalidStatus.endpoint.AppPort = 1
	if err := invalidStatus.doInput(casefile.Input{Path: "/health", Status: "created"}); err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Fatalf("invalid status error = %v", err)
	}
}

func TestSeedCaseRejectsUnknownType(t *testing.T) {
	r, _ := newRunner(t, &stubExec{}, Options{})
	c := mustParse(t, `
name: unknown seed
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
`)
	c.Seed.Type = "unknown"
	if err := r.seedCase(context.Background(), c); err == nil || !strings.Contains(err.Error(), "unknown seed type") {
		t.Fatalf("seedCase error = %v", err)
	}
}

func TestEvaluateCommonTextBranches(t *testing.T) {
	fails := evalCommonText("hello\nworld", casefile.AssertionCommon{
		Contains:    casefile.StringList{"hello"},
		NotContains: casefile.StringList{"missing"},
		Regex:       casefile.StringList{"world"},
		Count:       "== 2",
		Absent:      false,
	})
	if len(fails) != 0 {
		t.Fatalf("successful text assertions returned failures: %v", fails)
	}
	if fails := evalCommonText("hello", casefile.AssertionCommon{Absent: true}); len(fails) == 0 {
		t.Fatal("expected absent assertion to fail for non-empty output")
	}
}

func TestEvaluateStructuredAssertionBranches(t *testing.T) {
	trace := casefile.TraceAssertion{
		AssertionCommon: casefile.AssertionCommon{
			Contains: casefile.StringList{"trace"},
			Count:    "== 1",
		},
		MatchSpans: []casefile.MatchEntry{{Name: runnerStringPtr("operation")}},
	}
	rows := []assert.Row{{Name: "operation"}}
	if fails := evalTraceStructured("trace", trace, rows, 1, nil); len(fails) != 0 {
		t.Fatalf("structured trace assertions returned failures: %v", fails)
	}
	if fails := evalTraceStructured("trace", trace, nil, 0, fmt.Errorf("bad trace JSON")); len(fails) != 1 || fails[0].Rule != "match_spans" {
		t.Fatalf("trace parse failures = %v", fails)
	}
	trace = casefile.TraceAssertion{AssertionCommon: casefile.AssertionCommon{Absent: true}}
	if fails := evalTraceStructured("", trace, nil, 0, nil); len(fails) != 0 {
		t.Fatalf("absent structured trace returned failures: %v", fails)
	}

	common := casefile.AssertionCommon{Match: []casefile.MatchEntry{{Name: runnerStringPtr("row")}}, Count: "== 1"}
	if fails := evalCommonStructured("row", common, []assert.Row{{Name: "row"}}, 1, nil); len(fails) != 0 {
		t.Fatalf("structured assertions returned failures: %v", fails)
	}
	if fails := evalCommonStructured("row", common, nil, 0, fmt.Errorf("bad JSON")); len(fails) != 1 || fails[0].Rule != "match" {
		t.Fatalf("structured parse failures = %v", fails)
	}
	common.Absent = true
	common.Match = nil
	common.Count = ""
	if fails := evalCommonStructured("", common, nil, 0, nil); len(fails) != 0 {
		t.Fatalf("absent structured assertion returned failures: %v", fails)
	}
}

func runnerStringPtr(value string) *string { return &value }

func TestRunCaseCacheHitAndPassRecordsCache(t *testing.T) {
	casePath := filepath.Join(t.TempDir(), "case.yaml")
	if err := os.WriteFile(casePath, []byte(tracesCase), 0o600); err != nil {
		t.Fatal(err)
	}

	c := mustParse(t, tracesCase)
	c.SourcePath = casePath
	store, err := cache.New(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := newRunner(t, &stubExec{stdout: "found span service.name=svc"}, Options{Timeout: 50 * time.Millisecond, Interval: time.Millisecond, SeedSettleDelay: -1})
	r.WithCache(store, CacheContext{GCXVersion: "test", FixtureBytes: []byte("fixture")})
	if err := store.Record(r.cacheKey(c)); err != nil {
		t.Fatal(err)
	}
	if !r.RunCase(context.Background(), c) {
		t.Fatal("cache hit should pass")
	}

	uncached, err := cache.New(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := newRunner(t, &stubExec{stdout: "found span service.name=svc"}, Options{Timeout: 50 * time.Millisecond, Interval: time.Millisecond, SeedSettleDelay: -1})
	r2.WithCache(uncached, CacheContext{GCXVersion: "test", FixtureBytes: []byte("fixture")})
	if !r2.RunCase(context.Background(), c) {
		t.Fatal("uncached passing case should pass")
	}
	if hit, _ := uncached.Lookup(r2.cacheKey(c)); !hit {
		t.Fatal("passing case was not recorded in cache")
	}
}

func TestRunCaseInlineSeedSuccessAndPollErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := mustParse(t, `
name: inline seed success
seed:
  type: inline-otlp
  traces:
    - service: svc
      spans:
        - name: op
expected:
  traces:
    - traceql: '{}'
      contains: svc
`)
	r, buf := newRunner(t, &stubExec{stdout: "svc"}, Options{Timeout: 50 * time.Millisecond, Interval: time.Millisecond, SeedSettleDelay: -1})
	r.endpoint.OTLPHTTP = server.URL
	r.seeder.OTLPEndpoint = server.URL
	if !r.RunCase(context.Background(), c) {
		t.Fatalf("inline seed case should pass:\n%s", buf.String())
	}

	failingExec, _ := newRunner(t, &stubExec{err: errors.New("gcx unavailable")}, Options{Timeout: 5 * time.Millisecond, Interval: time.Millisecond, SeedSettleDelay: -1})
	if failingExec.pollAssert(context.Background(), mustParse(t, tracesCase), []string{"traces", "search"}, false, func(string, string, int) []assert.Failure { return nil }) {
		t.Fatal("pollAssert should fail when gcx execution errors")
	}
	nonZero, _ := newRunner(t, &stubExec{stderr: "gcx failed", exit: 1}, Options{Timeout: 5 * time.Millisecond, Interval: time.Millisecond, SeedSettleDelay: -1})
	if nonZero.pollAssert(context.Background(), mustParse(t, tracesCase), []string{"traces", "search"}, true, func(string, string, int) []assert.Failure { return nil }) {
		t.Fatal("pollAssert should fail for a non-zero gcx exit")
	}
}
