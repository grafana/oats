package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
)

// TestIntegration_FullPipelineWithFakeGCX wires the v2 chain end-to-end:
// discovery → seed (against an httptest OTLP stub) → engine (against the
// fake-gcx.sh shell script) → assertions → report. No real gcx, no real
// LGTM. The point is to catch wiring regressions across the package
// boundaries without standing up infrastructure.
func TestIntegration_FullPipelineWithFakeGCX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	// Build the temp workspace: an oats.toml plus one inline-otlp case.
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name    = "smoke"
cases   = ["cases/*.yaml"]
fixture = "remote-lgtm"

[fixture.remote-lgtm]
type     = "remote"
endpoint = "REPLACED_AT_RUNTIME"
`)
	writeFile(t, dir, "cases/inline.yaml", `oats: 2
name: inline seed end-to-end
seed:
  type: inline-otlp
  traces:
    - service: gcx-e2e-seed
      spans:
        - name: seed-operation
  logs:
    - service: gcx-e2e-seed
      body: seed-log-line
  metrics:
    - service: gcx-e2e-seed
      name: seed_counter
      value: 42
expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      match:
        - name: seed-operation
          attributes:
            service.name: gcx-e2e-seed
            trace_id:
              present: true
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      match:
        - name: seed-log-line
          attributes:
            service_name: gcx-e2e-seed
            trace_id:
              present: true
  metrics:
    - promql: 'seed_counter_total{service_name="gcx-e2e-seed"}'
      value: ">= 0"
      match:
        - name: seed_counter_total
          attributes:
            service_name: gcx-e2e-seed
`)

	// OTLP stub: accept any POST under /v1/* with 200.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	// Patch the endpoint into oats.toml after the stub URL is known.
	tomlPath := filepath.Join(dir, "oats.toml")
	rewrite(t, tomlPath, "REPLACED_AT_RUNTIME", stub.URL)

	cfg, err := discovery.Load(tomlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(discovery.Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 1 {
		t.Fatalf("expected one plan with one case, got %+v", plans)
	}

	// Wire the runner against the fake gcx.
	_, here, _, _ := runtime.Caller(0)
	fakeGCX := filepath.Join(filepath.Dir(here), "testdata", "fake-gcx.sh")

	exec := &engine.GCX{Binary: fakeGCX, Context: "smoke"}

	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, runner.Endpoint{
		GCXContext: "smoke",
		OTLPHTTP:   stub.URL,
	}, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Fatalf("case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Errorf("summary line missing or wrong:\n%s", buf.String())
	}
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rewrite(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(data, []byte(old), []byte(new)), 0o644); err != nil {
		t.Fatal(err)
	}
}
