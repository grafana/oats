package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/fixture"
	"github.com/grafana/oats/migrate"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
)

func TestResolveEndpoint_ComposeDefaults(t *testing.T) {
	ep, err := resolveEndpoint(discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}},
	}, fixture.Runtime{GCXConfig: "/tmp/gcx.yaml", OTLPHTTP: "http://127.0.0.1:4318"}, "", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if ep.GCXContext != "" || ep.GCXConfig != "/tmp/gcx.yaml" || ep.AppHost != "localhost" || ep.AppPort != 8080 || ep.OTLPHTTP != "http://127.0.0.1:4318" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestResolveEndpoint_UsesRuntimeAppHostPort(t *testing.T) {
	// The resolved app host port is surfaced uniformly through Runtime (compose
	// discovers an ephemeral port, k3d copies its configured one), and
	// resolveEndpoint applies it regardless of fixture type.
	ep, err := resolveEndpoint(discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{K3D: &casefile.K3DFixture{AppPort: 18080}},
	}, fixture.Runtime{GCXConfig: "/tmp/gcx.yaml", OTLPHTTP: "http://127.0.0.1:4318", AppHostPort: 18080}, "", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if ep.GCXContext != "" || ep.AppPort != 18080 {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestResolveEndpoint_RemoteUsesGCXGrafanaURL(t *testing.T) {
	config := filepath.Join(t.TempDir(), "gcx.yaml")
	writeFile(t, filepath.Dir(config), filepath.Base(config), `current-context: remote
contexts:
  remote:
    grafana:
      server: http://grafana.example.test:3000/
`)
	t.Setenv("GCX_CONFIG", config)

	ep, err := resolveEndpoint(discovery.Plan{
		Name:    "remote-basic",
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://otlp.example.test:4318"}},
	}, fixture.Runtime{}, "remote", "localhost", 8080, "")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if ep.GCXContext != "remote" || ep.OTLPHTTP != "http://otlp.example.test:4318" {
		t.Fatalf("unexpected remote endpoint: %+v", ep)
	}
	if !slices.Contains(ep.CustomCheckEnv, "OATS_GRAFANA_URL=http://grafana.example.test:3000") {
		t.Fatalf("remote custom-check env missing Grafana URL: %v", ep.CustomCheckEnv)
	}
}

func TestCloseFixture_EmitsTeardownEvent(t *testing.T) {
	rep := &recordingReporter{}
	fix := &fakeSuiteFixture{}
	plan := discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{}},
	}
	if err := closeFixture(rep, plan, fix); err != nil {
		t.Fatalf("closeFixture: %v", err)
	}
	if fix.closeCalls != 1 {
		t.Fatalf("expected Close once, got %d", fix.closeCalls)
	}
	if len(rep.events) != 1 || rep.events[0].Type != report.EventFixtureTeardown {
		t.Fatalf("expected one teardown event, got %+v", rep.events)
	}
	if rep.events[0].Suite != "smoke" || rep.events[0].FixtureType != "compose" {
		t.Fatalf("unexpected teardown event: %+v", rep.events[0])
	}
}

func TestCloseFixture_RemoteDoesNotEmitTeardownEvent(t *testing.T) {
	rep := &recordingReporter{}
	fix := &fakeSuiteFixture{}
	plan := discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{}},
	}
	if err := closeFixture(rep, plan, fix); err != nil {
		t.Fatalf("closeFixture: %v", err)
	}
	if fix.closeCalls != 1 {
		t.Fatalf("expected Close once, got %d", fix.closeCalls)
	}
	if len(rep.events) != 0 {
		t.Fatalf("expected no teardown events for remote fixture, got %+v", rep.events)
	}
}

func TestEmitFixtureStartAndReady(t *testing.T) {
	rep := &recordingReporter{}
	plan := discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{}},
	}
	start := emitFixtureStart(rep, plan)
	if start.IsZero() {
		t.Fatalf("expected non-zero start time")
	}
	if len(rep.events) != 1 || rep.events[0].Type != report.EventFixtureStart {
		t.Fatalf("expected one fixture.start event, got %+v", rep.events)
	}
	if rep.events[0].Suite != "smoke" || rep.events[0].FixtureType != "compose" {
		t.Fatalf("unexpected fixture.start event: %+v", rep.events[0])
	}

	emitFixtureReady(rep, plan, start.Add(-5*time.Millisecond))
	if len(rep.events) != 2 || rep.events[1].Type != report.EventFixtureReady {
		t.Fatalf("expected fixture.ready event, got %+v", rep.events)
	}
	if rep.events[1].DurationMs <= 0 {
		t.Fatalf("expected positive ready duration, got %+v", rep.events[1])
	}
}

func TestEmitFixtureStartAndReady_NoOpForRemote(t *testing.T) {
	rep := &recordingReporter{}
	plan := discovery.Plan{
		Name:    "smoke",
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{}},
	}
	start := emitFixtureStart(rep, plan)
	if start.IsZero() {
		t.Fatalf("expected non-zero start time")
	}
	emitFixtureReady(rep, plan, start)
	if len(rep.events) != 0 {
		t.Fatalf("expected no events for remote fixture lifecycle helpers, got %+v", rep.events)
	}
}

// TestIntegration_FullPipelineWithFakeGCX wires the v2 chain end-to-end:
// discovery → seed (against an httptest OTLP stub) → engine (against the
// fake-gcx.sh shell script) → assertions → report. No real gcx, no real
// LGTM. The point is to catch wiring regressions across the package
// boundaries without standing up infrastructure.

func fakeGCXPath(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	fakeGCX := filepath.Join(filepath.Dir(here), "testdata", "fake-gcx.sh")
	if err := os.Chmod(fakeGCX, 0o755); err != nil {
		t.Fatalf("chmod fake gcx: %v", err)
	}
	return fakeGCX
}

func TestIntegration_FullPipelineWithFakeGCX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	// Build the temp workspace: an oats-config.yaml plus one inline-otlp case.
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/inline.yaml", `name: inline seed end-to-end
fixture:
  remote:
    endpoint: "REPLACED_AT_RUNTIME"
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
      match_spans:
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

	// Patch the endpoint into the case's remote fixture after the stub URL is known.
	rewrite(t, filepath.Join(dir, "cases", "inline.yaml"), "REPLACED_AT_RUNTIME", stub.URL)

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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
	fakeGCX := fakeGCXPath(t)

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

func TestIntegration_AppSeedWithRemoteFixtureAndInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	var hits int
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPost || r.URL.Path != "/emit" {
			t.Fatalf("unexpected app request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer app.Close()
	appHost, appPort := splitHostPort(t, app.Listener.Addr().String())

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/app.yaml", `name: app seed end-to-end
fixture:
  remote:
    endpoint: "http://localhost:4318"
seed:
  type: app
input:
  - path: /emit
    method: POST
    status: "201"
expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      match_spans:
        - name: seed-operation
          attributes:
            service.name: gcx-e2e-seed
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      match:
        - name: seed-log-line
          attributes:
            service_name: gcx-e2e-seed
`)

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "", appHost, appPort, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	fakeGCX := fakeGCXPath(t)
	exec := &engine.GCX{Binary: fakeGCX, Context: ep.GCXContext}

	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})

	if !ok {
		t.Fatalf("case did not pass:\n%s", buf.String())
	}
	if hits == 0 {
		t.Fatalf("expected input endpoint to be hit")
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Errorf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_ProfileQueryWithFakeGCX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/profile.yaml", `name: profile query end-to-end
fixture:
  remote:
    endpoint: "http://localhost:4318"
seed:
  type: app
expected:
  profiles:
    - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{}'
      match:
        - match_type: regexp
          name: 'main|worker'
`)

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	fakeGCX := fakeGCXPath(t)
	exec := &engine.GCX{Binary: fakeGCX, Context: ep.GCXContext}

	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("profile case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_MigratedLegacyProfileRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy-profile.oats.yaml")
	writeFile(t, filepath.Dir(legacyPath), filepath.Base(legacyPath), `
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  profiles:
    - query: 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{}'
      flamebearers:
        regexp: 'main|worker'
`)
	migrated, _, err := migrate.ConvertFile(legacyPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/migrated.yaml", string(migrated))

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	fakeGCX := fakeGCXPath(t)
	exec := &engine.GCX{Binary: fakeGCX, Context: ep.GCXContext}

	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("migrated legacy profile case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_MigratedLegacyCaseRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.oats.yaml")
	writeFile(t, filepath.Dir(legacyPath), filepath.Base(legacyPath), `
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      equals: seed-operation
      attributes:
        service.name: gcx-e2e-seed
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      equals: seed-log-line
      attributes:
        service_name: gcx-e2e-seed
  metrics:
    - promql: 'seed_counter_total{service_name="gcx-e2e-seed"}'
      value: ">= 0"
`)
	migrated, _, err := migrate.ConvertFile(legacyPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/migrated.yaml", string(migrated))

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	fakeGCX := fakeGCXPath(t)
	exec := &engine.GCX{Binary: fakeGCX, Context: ep.GCXContext}

	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("migrated case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_MigratedLegacyCustomCheckRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script custom checks are POSIX-oriented")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy-custom.oats.yaml")
	writeFile(t, filepath.Dir(legacyPath), filepath.Base(legacyPath), `
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  custom-checks:
    - script: |
        #!/bin/sh
        exit 0
`)
	migrated, _, err := migrate.ConvertFile(legacyPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/migrated.yaml", string(migrated))

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	exec := &engine.GCX{Binary: "does-not-run", Context: ep.GCXContext}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("migrated custom-check case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_MigratedLegacyCustomCheckRelativePathRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script custom checks are POSIX-oriented")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy-custom-path.oats.yaml")
	writeFile(t, filepath.Dir(legacyPath), filepath.Base(legacyPath), `
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  custom-checks:
    - script: ./scripts/verify.sh
`)
	migrated, _, err := migrate.ConvertFile(legacyPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/migrated.yaml", string(migrated))
	writeFile(t, dir, "cases/scripts/verify.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(dir, "cases", "scripts", "verify.sh"), 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	exec := &engine.GCX{Binary: "does-not-run", Context: ep.GCXContext}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("migrated custom-check relative-path case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
	}
}

func TestIntegration_MigratedSingleMatrixCaseRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy-matrix.oats.yaml")
	writeFile(t, filepath.Dir(legacyPath), filepath.Base(legacyPath), `
oats-schema-version: 2
matrix:
  - name: docker
    docker-compose:
      files:
        - docker-compose.yml
expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      equals: seed-operation
      attributes:
        service.name: gcx-e2e-seed
      matrix-condition: docker
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      equals: should-not-run
      matrix-condition: k8s
  logs:
    - logql: '{service_name="gcx-e2e-seed"}'
      equals: seed-log-line
      attributes:
        service_name: gcx-e2e-seed
      matrix-condition: docker
`)
	migrated, warnings, err := migrate.ConvertFile(legacyPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, `flattened single matrix entry "docker"`) {
		t.Fatalf("expected flattening warning, got: %v", warnings)
	}

	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/migrated.yaml", string(migrated))

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
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
	if got := plans[0].Cases[0].Name; got != "legacy matrix.oats [docker]" {
		t.Fatalf("unexpected migrated case name: %q", got)
	}
	if got := len(plans[0].Cases[0].Expected.Traces); got != 1 {
		t.Fatalf("expected k8s-only assertion to be filtered out, got %d traces", got)
	}

	// Compose fixtures derive their gcx context from the booted stack; these
	// tests don't boot one, so pass the context explicitly via the override
	// (fake-gcx ignores the value). Harmless for the remote profile test, whose
	// endpoint still comes from its fixture.
	ep, err := resolveEndpoint(plans[0], fixture.Runtime{}, "oats-test", "localhost", 8080, "http://localhost:4318")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}

	fakeGCX := fakeGCXPath(t)
	exec := &engine.GCX{Binary: fakeGCX, Context: ep.GCXContext}
	var buf bytes.Buffer
	rep := report.NewTextReporter(&buf, report.VerboseDefault)
	rep.Emit(report.Event{Type: report.EventRunStart, OatsVersion: "test", SchemaVersion: report.SchemaVersion})

	r := runner.New(exec, rep, ep, runner.Options{
		Timeout:         500 * time.Millisecond,
		Interval:        20 * time.Millisecond,
		SeedSettleDelay: 5 * time.Millisecond,
	})

	ok := r.RunCase(context.Background(), plans[0].Cases[0])
	rep.Emit(report.Event{Type: report.EventRunEnd})
	if !ok {
		t.Fatalf("migrated matrix case did not pass:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "PASS 1/1") {
		t.Fatalf("summary line missing or wrong:\n%s", buf.String())
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

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portText, err)
	}
	return host, port
}

type fakeSuiteFixture struct {
	closeCalls int
	closeErr   error
}

func (f *fakeSuiteFixture) Close() error {
	f.closeCalls++
	return f.closeErr
}

type recordingReporter struct {
	events []report.Event
}

func (r *recordingReporter) Emit(e report.Event) {
	r.events = append(r.events, e)
}

func (r *recordingReporter) Close() error { return nil }
