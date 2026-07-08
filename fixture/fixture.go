// Package fixture owns the lifecycle of the observability backends a suite
// runs against: booting a docker-compose or k3d stack, waiting for it to be
// ready, exposing the resolved endpoints as a Runtime, and tearing it down.
// The CLI orchestrates suites and reporting; this package abstracts "stand up
// the stack the cases need" behind a small, pluggable surface.
package fixture

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
)

// Seams overridable in tests.
var (
	newComposeSuite = func(files []string, env []string) (Handle, error) {
		return compose.SuiteFiles(files, env)
	}
	newKubernetesEndpoint = func(plan discovery.Plan, ports remote.PortsConfig) *remote.Endpoint {
		sourceDir := plan.FixtureSourceDir
		if sourceDir == "" {
			sourceDir = "."
		}
		model := &kubernetes.Kubernetes{
			Dir:              plan.Fixture.K8sDir,
			AppService:       plan.Fixture.AppService,
			AppDockerFile:    plan.Fixture.AppDockerFile,
			AppDockerContext: plan.Fixture.AppDockerContext,
			AppDockerTag:     plan.Fixture.AppDockerTag,
			AppDockerPort:    plan.Fixture.AppPort,
			ImportImages:     plan.Fixture.ImportImages,
		}
		return kubernetes.NewEndpoint("localhost", model, ports, plan.Suite.Name, sourceDir)
	}
	waitForGrafanaToken = waitForGrafanaTokenImpl
	lookupComposePort   = dockerComposePort
)

// Runtime carries the resolved coordinates of a booted fixture back to the
// caller: where the backends live, the gcx config to talk to them, the compose
// project (for teardown/labels), and whether the fixture is parallel-safe.
type Runtime struct {
	GrafanaURL       string
	OTLPHTTP         string
	PyroscopeURL     string
	AppHostPort      int
	CustomCheckEnv   []string
	ComposeFiles     []string
	ComposeProject   string
	GCXConfig        string
	ParallelSafe     bool
	ParallelDisabled string
}

// Handle is a booted fixture that can be torn down.
type Handle interface {
	Close() error
}

type startableHandle interface {
	Handle
	Up() error
}

// Start boots the fixture declared by the plan and returns a Handle for
// teardown plus the resolved Runtime. Remote/empty fixtures need no boot and
// return a nil Handle.
func Start(ctx context.Context, plan discovery.Plan) (Handle, Runtime, error) {
	switch plan.Fixture.Type {
	case "", "remote":
		return nil, Runtime{ParallelSafe: true}, nil
	case "compose":
		return startCompose(plan)
	case "k3d":
		return startK3D(ctx, plan)
	default:
		return nil, Runtime{}, fmt.Errorf("fixture type %q is not supported in oats", plan.Fixture.Type)
	}
}

// WaitForReady blocks until the fixture's Grafana and OTLP endpoints answer, or
// the per-endpoint timeout elapses. Remote/empty fixtures are assumed ready.
func WaitForReady(plan discovery.Plan, rt Runtime) error {
	switch plan.Fixture.Type {
	case "compose", "k3d":
		if err := waitForHTTP(rt.GrafanaURL+"/api/health", 2*time.Minute); err != nil {
			return fmt.Errorf("wait for grafana: %w", err)
		}
		if err := waitForHTTP(rt.OTLPHTTP, 2*time.Minute); err != nil {
			return fmt.Errorf("wait for otlp-http: %w", err)
		}
	}
	return nil
}

// SupportsParallel reports whether a suite on this fixture can run alongside
// other suites, and if not, a human-readable reason.
func SupportsParallel(plan discovery.Plan) (bool, string) {
	switch plan.Fixture.Type {
	case "", "remote":
		return true, ""
	case "compose":
		if plan.Fixture.Template != "lgtm" {
			return false, "compose fixtures are only parallel-safe when OATS owns the LGTM ports via template=lgtm"
		}
		for _, c := range plan.Cases {
			// App seeds are parallel-safe only when OATS can give the app an
			// ephemeral host port instead of a shared fixed one — which requires
			// fixture.app_service (+ app_port) so the published port can be
			// discovered. Without it the app falls back to the fixed --app-port
			// and parallel suites would collide.
			if c.Seed.Type == "app" && (plan.Fixture.AppService == "" || plan.Fixture.AppPort == 0) {
				return false, "compose app-seed suites need fixture.app_service and app_port so OATS can publish an ephemeral app port; otherwise they share a fixed app port"
			}
		}
		for _, file := range extraComposeFiles(plan) {
			if fixed, err := composeFilePublishesFixedHostPorts(file); err != nil {
				return false, fmt.Sprintf("compose port inspection failed for %s: %v", file, err)
			} else if fixed {
				return false, fmt.Sprintf("compose file %s publishes fixed host ports", filepath.Base(file))
			}
		}
		return true, ""
	case "k3d":
		return false, "k3d fixtures currently use shared localhost port-forwards"
	default:
		return false, "fixture type is not parallel-safe"
	}
}

func startSuiteFixture(fix Handle) error {
	startable, ok := fix.(startableHandle)
	if !ok {
		return fmt.Errorf("fixture does not support startup")
	}
	return startable.Up()
}

type endpointFixture struct {
	ep      *remote.Endpoint
	cleanup func() error
}

func (e endpointFixture) Close() error {
	err := e.ep.Stop(context.Background())
	if e.cleanup != nil {
		if cleanupErr := e.cleanup(); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}
	return err
}

type composeFixture struct {
	suite   Handle
	cleanup func() error
}

func (c composeFixture) Close() error {
	err := c.suite.Close()
	if c.cleanup != nil {
		if cleanupErr := c.cleanup(); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}
	return err
}

// removeIfExists deletes path, ignoring a not-exist error so cleanup is
// idempotent.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// chainCleanup returns a cleanup that runs first then second, returning the
// first non-nil error. Either argument may be nil.
func chainCleanup(first, second func() error) func() error {
	return func() error {
		var err error
		if first != nil {
			err = first()
		}
		if second != nil {
			if e := second(); e != nil && err == nil {
				err = e
			}
		}
		return err
	}
}

func writeLocalGCXConfig(grafanaURL string) (string, error) {
	cfg := fmt.Sprintf(`current-context: local
contexts:
  local:
    grafana:
      server: %s
      user: admin
      password: admin
      org-id: 1
      auth-method: basic
    datasources:
      prometheus: prometheus
      loki: loki
      tempo: tempo
      pyroscope: pyroscope
`, grafanaURL)
	f, err := os.CreateTemp("", "oats-gcx-*.yaml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func waitForGrafanaTokenImpl(plan discovery.Plan) (string, error) {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var token string
		var err error
		switch plan.Fixture.Type {
		case "compose":
			token, err = readComposeGrafanaToken(plan)
		case "k3d":
			token, err = readK3DGrafanaToken()
		default:
			return "", nil
		}
		if err == nil && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token), nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("timed out waiting for Grafana service-account token")
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %T", ln.Addr())
	}
	return addr.Port, nil
}

func waitForHTTP(url string, timeout time.Duration) error {
	// Bound each probe so a target that accepts TCP but never responds can't
	// block a single GET past the overall deadline.
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) //nolint:gosec
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s", url)
}
