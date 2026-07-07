package fixture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/runner"
	"github.com/grafana/oats/testhelpers/remote"
)

func TestResolveComposeFiles(t *testing.T) {
	got, cleanup, err := resolveComposeFiles("/tmp/work", discovery.FixtureConfig{Type: "compose", ComposeFile: "stack/compose.yml"})
	if err != nil {
		t.Fatalf("resolveComposeFiles compose_file: %v", err)
	}
	if cleanup != nil {
		t.Fatalf("unexpected cleanup for compose_file fixture")
	}
	if want := []string{"/tmp/work/stack/compose.yml"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %q want %q", got, want)
	}

	got, cleanup, err = resolveComposeFiles("/tmp/work", discovery.FixtureConfig{Type: "compose", ComposeFiles: []string{"a.yml", "b.yml"}})
	if err != nil {
		t.Fatalf("resolveComposeFiles compose_files: %v", err)
	}
	if cleanup != nil {
		t.Fatalf("unexpected cleanup for compose_files fixture")
	}
	if len(got) != 2 || got[0] != "/tmp/work/a.yml" || got[1] != "/tmp/work/b.yml" {
		t.Fatalf("unexpected compose_files resolution: %v", got)
	}

	got, cleanup, err = resolveComposeFiles("/tmp/work", discovery.FixtureConfig{Type: "compose", Template: "lgtm"})
	if err != nil {
		t.Fatalf("resolveComposeFiles template=lgtm: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected cleanup for template=lgtm fixture")
	}
	defer func() { _ = cleanup() }()
	if len(got) != 1 || !strings.Contains(filepath.Base(got[0]), ".oats.lgtm.") || !strings.HasSuffix(got[0], ".compose.yml") {
		t.Fatalf("unexpected template=lgtm resolution: %v", got)
	}
}

func TestStart_ComposeLifecycle(t *testing.T) {
	oldFactory := newComposeSuite
	oldLookup := lookupComposePort
	defer func() { newComposeSuite = oldFactory }()
	defer func() { lookupComposePort = oldLookup }()

	var gotFiles, gotEnv []string
	fake := &fakeHandle{}
	newComposeSuite = func(files []string, env []string) (Handle, error) {
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

	fix, _, err := Start(context.Background(), discovery.Plan{
		Suite: discovery.SuiteConfig{Name: "smoke", Fixture: "local"},
		Fixture: discovery.FixtureConfig{
			Type:         "compose",
			ComposeFiles: []string{"a.yml", "b.yml"},
			Env:          []string{"FOO=bar"},
		},
		FixtureSourceDir: "/tmp/work",
	})
	if err != nil {
		t.Fatalf("Start compose: %v", err)
	}
	if fake.upCalls != 1 {
		t.Fatalf("expected Up once, got %d", fake.upCalls)
	}
	if want := []string{"/tmp/work/a.yml", "/tmp/work/b.yml"}; !equalStrings(gotFiles, want) {
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
	oldFactory := newComposeSuite
	defer func() { newComposeSuite = oldFactory }()

	newComposeSuite = func(files []string, env []string) (Handle, error) {
		return &fakeHandle{upErr: fmt.Errorf("boom")}, nil
	}

	_, _, err := Start(context.Background(), discovery.Plan{
		Suite:            discovery.SuiteConfig{Name: "smoke", Fixture: "local"},
		Fixture:          discovery.FixtureConfig{Type: "compose", ComposeFile: "compose.yml"},
		FixtureSourceDir: "/tmp/work",
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

	fix, _, err := Start(context.Background(), discovery.Plan{
		Suite: discovery.SuiteConfig{Name: "cluster-smoke", Fixture: "cluster"},
		Fixture: discovery.FixtureConfig{
			Type:             "k3d",
			K8sDir:           "k8s",
			AppService:       "dice",
			AppDockerFile:    "Dockerfile",
			AppDockerContext: ".",
			AppDockerTag:     "dice:test",
			AppPort:          18080,
			ImportImages:     []string{"busybox:latest"},
		},
		FixtureSourceDir: "/tmp/work",
	})
	if err != nil {
		t.Fatalf("Start k3d: %v", err)
	}
	if starts != 1 {
		t.Fatalf("expected one endpoint start, got %d", starts)
	}
	if capturedPlan.FixtureSourceDir != "/tmp/work" || capturedPlan.Suite.Name != "cluster-smoke" || capturedPlan.Fixture.AppPort != 18080 {
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
		Suite: discovery.SuiteConfig{Name: "cluster-smoke", Fixture: "cluster"},
		Fixture: discovery.FixtureConfig{
			Type:             "k3d",
			K8sDir:           "k8s",
			AppService:       "dice",
			AppDockerFile:    "Dockerfile",
			AppDockerContext: ".",
			AppDockerTag:     "dice:test",
			AppPort:          18080,
		},
		FixtureSourceDir: "/tmp/work",
	})
	if err == nil || !strings.Contains(err.Error(), "cluster boom") {
		t.Fatalf("expected k3d startup error, got %v", err)
	}
}

func TestK3DCheckEnv_UsesConfiguredPorts(t *testing.T) {
	got := k3dCheckEnv(runner.Endpoint{AppHost: "localhost", AppPort: 18080}, remote.PortsConfig{
		GrafanaHTTPPort:   13000,
		OTLPHTTPPort:      14318,
		PyroscopeHttpPort: 14040,
	})
	for _, want := range []string{
		"OATS_APP_URL=http://localhost:18080",
		"OATS_GRAFANA_URL=http://localhost:13000",
		"OATS_OTLP_HTTP=http://localhost:14318",
		"OATS_PYROSCOPE_URL=http://localhost:14040",
	} {
		if !containsString(got, want) {
			t.Fatalf("k3dCheckEnv missing %q in %v", want, got)
		}
	}
}

func TestResolveComposeFiles_UnsupportedTemplate(t *testing.T) {
	_, _, err := resolveComposeFiles("/tmp/work", discovery.FixtureConfig{Type: "compose", Template: "weird"})
	if err == nil || !strings.Contains(err.Error(), `unsupported compose fixture template "weird"`) {
		t.Fatalf("expected unsupported template error, got %v", err)
	}
}

func TestResolveComposeFiles_MissingConfig(t *testing.T) {
	_, _, err := resolveComposeFiles("/tmp/work", discovery.FixtureConfig{Type: "compose"})
	if err == nil || !strings.Contains(err.Error(), "compose fixture requires compose_file, compose_files, or supported template") {
		t.Fatalf("expected missing compose config error, got %v", err)
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
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name = "parallel-safe"
cases = ["cases/*.yaml"]
fixture = "stack"

[fixture.stack]
type = "compose"
template = "lgtm"
compose_file = "docker-compose.oats.yml"
`)
	writeFile(t, dir, "docker-compose.oats.yml", `services:
  app:
    image: alpine
    command: ["sh", "-c", "sleep 1"]
`)
	writeFile(t, dir, "cases/a.yaml", `oats-schema-version: 3
name: a
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

	cfg, err := discovery.Load(filepath.Join(dir, "oats.toml"))
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

// TestWaitForGrafanaToken_DefaultFixtureReturnsEmpty exercises the token seam
// for a fixture type that has no token to read: it returns immediately with an
// empty token and no error.
func TestWaitForGrafanaToken_DefaultFixtureReturnsEmpty(t *testing.T) {
	token, err := waitForGrafanaToken(discovery.Plan{
		Fixture: discovery.FixtureConfig{Type: "remote"},
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
