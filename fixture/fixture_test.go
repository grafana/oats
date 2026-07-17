package fixture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/runner"
	"github.com/grafana/oats/testhelpers"
	"github.com/grafana/oats/testhelpers/remote"
)

func isBuiltinLGTMFile(path string) bool {
	return strings.Contains(filepath.Base(path), ".oats.lgtm.") && strings.HasSuffix(path, ".compose.yml")
}

func TestResolveComposeFiles(t *testing.T) {
	dir := t.TempDir()

	// template=none with a single file: only the user's file, no builtin, no
	// cleanup.
	got, cleanup, err := resolveComposeFiles(dir, &casefile.ComposeFixture{Template: "none", File: "stack/compose.yml"})
	if err != nil {
		t.Fatalf("resolveComposeFiles template=none file: %v", err)
	}
	if cleanup != nil {
		t.Fatalf("unexpected cleanup for template=none file fixture")
	}
	if want := []string{filepath.Join(dir, "stack", "compose.yml")}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %q want %q", got, want)
	}

	// template=none with files: only the user's files, no builtin.
	got, cleanup, err = resolveComposeFiles(dir, &casefile.ComposeFixture{Template: "none", Files: []string{"a.yml", "b.yml"}})
	if err != nil {
		t.Fatalf("resolveComposeFiles template=none files: %v", err)
	}
	if cleanup != nil {
		t.Fatalf("unexpected cleanup for template=none files fixture")
	}
	if len(got) != 2 || got[0] != filepath.Join(dir, "a.yml") || got[1] != filepath.Join(dir, "b.yml") {
		t.Fatalf("unexpected files resolution: %v", got)
	}

	// No template + a user file: the builtin lgtm compose is merged in ahead of
	// the user's file (default template=lgtm), with cleanup for the temp file.
	got, cleanup, err = resolveComposeFiles(dir, &casefile.ComposeFixture{File: "stack/compose.yml"})
	if err != nil {
		t.Fatalf("resolveComposeFiles default template + file: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected cleanup for default (lgtm) fixture")
	}
	if len(got) != 2 || !isBuiltinLGTMFile(got[0]) || got[1] != filepath.Join(dir, "stack", "compose.yml") {
		_ = cleanup()
		t.Fatalf("unexpected default-template resolution: %v", got)
	}
	_ = cleanup()

	// Explicit template=lgtm with no file: just the builtin lgtm compose.
	got, cleanup, err = resolveComposeFiles(dir, &casefile.ComposeFixture{Template: "lgtm"})
	if err != nil {
		t.Fatalf("resolveComposeFiles template=lgtm: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected cleanup for template=lgtm fixture")
	}
	defer func() { _ = cleanup() }()
	if len(got) != 1 || !isBuiltinLGTMFile(got[0]) {
		t.Fatalf("unexpected template=lgtm resolution: %v", got)
	}
}

func TestStart_ComposeLifecycle(t *testing.T) {
	oldFactory := newComposeStack
	oldLookup := lookupComposePort
	defer func() { newComposeStack = oldFactory }()
	defer func() { lookupComposePort = oldLookup }()

	var gotFiles, gotEnv []string
	fake := &fakeHandle{}
	newComposeStack = func(files []string, env []string) (Handle, error) {
		gotFiles = append([]string(nil), files...)
		gotEnv = append([]string(nil), env...)
		return fake, nil
	}
	lookupComposePort = func(files []string, env []string, service, containerPort string) (string, error) {
		switch containerPort {
		case "3000":
			return "43000", nil
		case "4318":
			return "44318", nil
		case "4040":
			return "44040", nil
		default:
			return "", fmt.Errorf("unexpected port %s", containerPort)
		}
	}

	sourceDir := t.TempDir()
	fix, _, err := Start(context.Background(), discovery.Plan{
		Name: "smoke",
		Fixture: casefile.FixtureConfig{
			Compose: &casefile.ComposeFixture{
				// template=none keeps the resolved file list exactly a.yml/b.yml
				// so this lifecycle test isn't perturbed by the builtin lgtm file.
				Template: "none",
				Files:    []string{"a.yml", "b.yml"},
				Env:      []string{"FOO=bar"},
			},
		},
		FixtureSourceDir: sourceDir,
	})
	if err != nil {
		t.Fatalf("Start compose: %v", err)
	}
	if fake.upCalls != 1 {
		t.Fatalf("expected Up once, got %d", fake.upCalls)
	}
	if want := []string{filepath.Join(sourceDir, "a.yml"), filepath.Join(sourceDir, "b.yml")}; !equalStrings(gotFiles, want) {
		t.Fatalf("compose files: got %v want %v", gotFiles, want)
	}
	if len(gotEnv) != 2 || gotEnv[0] != "FOO=bar" || !strings.HasPrefix(gotEnv[1], "COMPOSE_PROJECT_NAME=oats-smoke-") {
		t.Fatalf("compose env: got %v", gotEnv)
	}
	if err := fix.Close(); err != nil {
		t.Fatalf("fixture close: %v", err)
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected Close once, got %d", fake.closeCalls)
	}
}

func TestStart_ComposeStartFailure(t *testing.T) {
	oldFactory := newComposeStack
	defer func() { newComposeStack = oldFactory }()

	newComposeStack = func(files []string, env []string) (Handle, error) {
		return &fakeHandle{upErr: fmt.Errorf("boom")}, nil
	}

	_, _, err := Start(context.Background(), discovery.Plan{
		Name:             "smoke",
		Fixture:          casefile.FixtureConfig{Compose: &casefile.ComposeFixture{File: "compose.yml"}},
		FixtureSourceDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected compose startup error, got %v", err)
	}
}

func TestStart_K3DLifecycle(t *testing.T) {
	oldFactory := newKubernetesEndpoint
	defer func() { newKubernetesEndpoint = oldFactory }()

	var capturedPlan discovery.Plan
	var starts, stops int
	var capturedPorts remote.PortsConfig
	newKubernetesEndpoint = func(plan discovery.Plan, ports remote.PortsConfig) *remote.Endpoint {
		capturedPlan = plan
		capturedPorts = ports
		return remote.NewEndpoint("localhost", remote.PortsConfig{}, func(ctx context.Context) error {
			starts++
			return nil
		}, func(ctx context.Context) error {
			stops++
			return nil
		}, nil)
	}

	sourceDir := t.TempDir()
	fix, rt, err := Start(context.Background(), discovery.Plan{
		Name: "cluster-smoke",
		Fixture: casefile.FixtureConfig{
			K3D: &casefile.K3DFixture{
				K8sDir:           "k8s",
				AppService:       "dice",
				AppDockerFile:    "Dockerfile",
				AppDockerContext: ".",
				AppDockerTag:     "dice:test",
				AppPort:          18080,
				ImportImages:     []string{"busybox:latest"},
			},
		},
		FixtureSourceDir: sourceDir,
	})
	if err != nil {
		t.Fatalf("Start k3d: %v", err)
	}
	if rt.AppHostPort != 18080 {
		t.Fatalf("expected Runtime.AppHostPort=18080 from fixture config, got %d", rt.AppHostPort)
	}
	if starts != 1 {
		t.Fatalf("expected one endpoint start, got %d", starts)
	}
	if capturedPlan.FixtureSourceDir != sourceDir || capturedPlan.Name != "cluster-smoke" || capturedPlan.Fixture.K3D.AppPort != 18080 {
		t.Fatalf("unexpected endpoint factory args: plan=%+v", capturedPlan)
	}
	if capturedPorts.GrafanaHTTPPort == 0 || capturedPorts.OTLPHTTPPort == 0 || capturedPorts.LokiHttpPort == 0 || capturedPorts.PrometheusHTTPPort == 0 || capturedPorts.TempoHTTPPort == 0 || capturedPorts.PyroscopeHttpPort == 0 {
		t.Fatalf("expected allocated k3d ports, got %+v", capturedPorts)
	}
	if err := fix.Close(); err != nil {
		t.Fatalf("fixture close: %v", err)
	}
	if stops != 1 {
		t.Fatalf("expected one endpoint stop, got %d", stops)
	}
}

func TestStart_K3DStartFailure(t *testing.T) {
	oldFactory := newKubernetesEndpoint
	defer func() { newKubernetesEndpoint = oldFactory }()

	newKubernetesEndpoint = func(plan discovery.Plan, ports remote.PortsConfig) *remote.Endpoint {
		return remote.NewEndpoint("localhost", remote.PortsConfig{}, func(ctx context.Context) error {
			return fmt.Errorf("cluster boom")
		}, func(ctx context.Context) error {
			return nil
		}, nil)
	}

	_, _, err := Start(context.Background(), discovery.Plan{
		Name: "cluster-smoke",
		Fixture: casefile.FixtureConfig{
			K3D: &casefile.K3DFixture{
				K8sDir:           "k8s",
				AppService:       "dice",
				AppDockerFile:    "Dockerfile",
				AppDockerContext: ".",
				AppDockerTag:     "dice:test",
				AppPort:          18080,
			},
		},
		FixtureSourceDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "cluster boom") {
		t.Fatalf("expected k3d startup error, got %v", err)
	}
}

func TestK3DCheckEnv_UsesConfiguredPorts(t *testing.T) {
	got := k3dCheckEnv(runner.Endpoint{AppHost: testhelpers.LocalhostIPv4, AppPort: 18080}, remote.PortsConfig{
		GrafanaHTTPPort:   13000,
		OTLPHTTPPort:      14318,
		PyroscopeHttpPort: 14040,
	})
	for _, want := range []string{
		"OATS_APP_URL=http://127.0.0.1:18080",
		"OATS_GRAFANA_URL=http://127.0.0.1:13000",
		"OATS_OTLP_HTTP=http://127.0.0.1:14318",
		"OATS_PYROSCOPE_URL=http://127.0.0.1:14040",
	} {
		if !containsString(got, want) {
			t.Fatalf("k3dCheckEnv missing %q in %v", want, got)
		}
	}
}

func TestResolveComposeFiles_UnsupportedTemplate(t *testing.T) {
	_, _, err := resolveComposeFiles(t.TempDir(), &casefile.ComposeFixture{Template: "weird"})
	if err == nil || !strings.Contains(err.Error(), `unsupported compose template "weird"`) {
		t.Fatalf("expected unsupported template error, got %v", err)
	}
}

func TestResolveComposeFiles_TemplateNoneRequiresFile(t *testing.T) {
	_, _, err := resolveComposeFiles(t.TempDir(), &casefile.ComposeFixture{Template: "none"})
	if err == nil || !strings.Contains(err.Error(), "compose template=none requires file or files") {
		t.Fatalf("expected template=none missing-file error, got %v", err)
	}
}

func TestComposeFilePublishesFixedHostPorts(t *testing.T) {
	dir := t.TempDir()
	fixed := filepath.Join(dir, "fixed.yml")
	random := filepath.Join(dir, "random.yml")
	writeFile(t, dir, "fixed.yml", `services:
  app:
    image: alpine
    ports:
      - "8080:8080"
`)
	writeFile(t, dir, "random.yml", `services:
  app:
    image: alpine
    ports:
      - "8080"
`)
	got, err := composeFilePublishesFixedHostPorts(fixed)
	if err != nil {
		t.Fatalf("composeFilePublishesFixedHostPorts fixed: %v", err)
	}
	if !got {
		t.Fatalf("expected fixed host port detection for %s", fixed)
	}
	got, err = composeFilePublishesFixedHostPorts(random)
	if err != nil {
		t.Fatalf("composeFilePublishesFixedHostPorts random: %v", err)
	}
	if got {
		t.Fatalf("did not expect fixed host port detection for %s", random)
	}
}

func TestSupportsParallel_ComposeTemplateLGTM(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/docker-compose.oats.yml", `services:
  app:
    image: alpine
    command: ["sh", "-c", "sleep 1"]
`)
	writeFile(t, dir, "cases/a.yaml", `name: a
fixture:
  compose:
    template: lgtm
    file: docker-compose.oats.yml
seed:
  type: inline-otlp
  logs:
    - service: a
      body: line
expected:
  logs:
    - logql: '{service_name="a"}'
      contains: line
`)

	cfg, err := discovery.Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(discovery.Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	safe, reason := SupportsParallel(plans[0])
	if !safe {
		t.Fatalf("expected plan to be parallel-safe, got false: %s", reason)
	}
}

// TestSupportsParallel_AppSeedRequiresAppService checks the gate that makes
// app-seed compose groups parallel-safe: only when fixture.app_service and
// app_port are set (so OATS can publish + discover an ephemeral app port) is the
// group safe; otherwise it would share a fixed app port with its peers.
func TestSupportsParallel_AppSeedRequiresAppService(t *testing.T) {
	appCase := &casefile.Case{Seed: casefile.Seed{Type: "app"}}

	// app seed, no app_service → not parallel-safe.
	noService := discovery.Plan{
		Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}},
		Cases:   []*casefile.Case{appCase},
	}
	if safe, reason := SupportsParallel(noService); safe {
		t.Fatalf("app-seed without app_service should not be parallel-safe, got safe (%s)", reason)
	}

	// app seed with app_service + app_port → parallel-safe.
	withService := discovery.Plan{
		Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm", AppService: "app", AppPort: 8080}},
		Cases:   []*casefile.Case{appCase},
	}
	if safe, reason := SupportsParallel(withService); !safe {
		t.Fatalf("app-seed with app_service+app_port should be parallel-safe: %s", reason)
	}
}

// TestWaitForGrafanaToken_DefaultFixtureReturnsEmpty exercises the token seam
// for a fixture type that has no token to read: it returns immediately with an
// empty token and no error.
func TestWaitForGrafanaToken_DefaultFixtureReturnsEmpty(t *testing.T) {
	token, err := waitForGrafanaToken(discovery.Plan{
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{}},
	})
	if err != nil {
		t.Fatalf("waitForGrafanaToken: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token for remote fixture, got %q", token)
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

type fakeHandle struct {
	upCalls    int
	closeCalls int
	upErr      error
	closeErr   error
}

func (f *fakeHandle) Up() error {
	f.upCalls++
	return f.upErr
}

func (f *fakeHandle) Close() error {
	f.closeCalls++
	return f.closeErr
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
