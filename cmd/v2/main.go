// Command oats is the OATS binary entry point.
//
// This command currently lives under cmd/v2 while the repository still keeps
// the legacy root entrypoint around for existing tests. The gcx-driven CLI is
// intended to replace that legacy path.
//
// Usage:
//
//	oats [flags]
//
// Flags (subset):
//
//	--config       Path to oats.toml (default ./oats.toml)
//	--gcx          Path to gcx binary (default "gcx" on PATH)
//	--list         Print the run plan and exit (no execution)
//	--format       Output format: "text" (default) or "ndjson"
//	-v / -vv / -vvv  Progressive verbosity (passes / commands / lifecycle)
//	--suite        Comma-separated suite names to include
//	--tags         Comma-separated tag any-match filter
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/migrate"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
)

var (
	newComposeSuite = func(files []string, env []string) (suiteFixture, error) {
		return compose.SuiteFiles(files, env)
	}
	newKubernetesEndpoint = func(sourceDir string, plan discovery.Plan) *remote.Endpoint {
		model := &kubernetes.Kubernetes{
			Dir:              filepath.Join(sourceDir, plan.Fixture.K8sDir),
			AppService:       plan.Fixture.AppService,
			AppDockerFile:    plan.Fixture.AppDockerFile,
			AppDockerContext: plan.Fixture.AppDockerContext,
			AppDockerTag:     plan.Fixture.AppDockerTag,
			AppDockerPort:    plan.Fixture.AppPort,
			ImportImages:     plan.Fixture.ImportImages,
		}
		return kubernetes.NewEndpoint("localhost", model, remote.PortsConfig{
			PrometheusHTTPPort: 9090,
			LokiHttpPort:       3100,
			TempoHTTPPort:      3200,
			PyroscopeHttpPort:  4040,
		}, plan.Suite.Name, sourceDir)
	}
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int {
	configPath := flag.String("config", "oats.toml", "path to oats.toml")
	gcxBin := flag.String("gcx", "gcx", "path to gcx binary (PATH-resolved if a bare name)")
	listOnly := flag.Bool("list", false, "print the run plan and exit (no execution)")
	migratePath := flag.String("migrate", "", "convert one legacy OATS yaml file and print the result to stdout")
	format := flag.String("format", "text", "output format: text | ndjson")
	suiteFilterStr := flag.String("suite", "", "comma-separated suite names")
	tagFilterStr := flag.String("tags", "", "comma-separated tag any-match")
	timeout := flag.Duration("timeout", 30*time.Second, "per-assertion timeout")
	interval := flag.Duration("interval", 500*time.Millisecond, "polling interval")
	absentTimeout := flag.Duration("absent-timeout", 10*time.Second, "absence-check window")
	seedSettle := flag.Duration("seed-settle", 2*time.Second, "post-seed wait before first assertion")
	gcxContextOverride := flag.String("gcx-context", "", "override the gcx --context value (otherwise derived from fixture endpoint)")
	appHost := flag.String("app-host", "localhost", "application host for driving case input requests")
	appPort := flag.Int("app-port", 8080, "application port for driving case input requests")
	otlpHTTP := flag.String("otlp-http", "http://localhost:4318", "OTLP/HTTP base URL for inline-otlp seed mode")
	noCache := flag.Bool("no-cache", false, "disable the skip-when-unchanged cache for this run")
	cacheDir := flag.String("cache-dir", defaultCacheDir(), "directory for the skip-when-unchanged cache")

	var verbose int
	flag.IntVar(&verbose, "v", 0, "verbosity (0-3)")

	flag.Parse()

	if *migratePath != "" {
		out, warnings, err := migrate.ConvertFile(*migratePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "migrate warning:", w)
		}
		fmt.Print(string(out))
		return 0
	}

	cfg, err := discovery.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	filter := discovery.Filter{
		Suites: splitCSV(*suiteFilterStr),
		Tags:   splitCSV(*tagFilterStr),
	}

	if *listOnly {
		fmt.Print(cfg.Summary())
		return 0
	}

	plans, err := cfg.PlanRun(filter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(plans) == 0 {
		fmt.Fprintln(os.Stderr, "no suites matched the filter")
		return 2
	}

	rep := newReporter(os.Stdout, *format, verbosityFromInt(verbose))
	defer func() { _ = rep.Close() }()

	rep.Emit(report.Event{
		Type:          report.EventRunStart,
		OatsVersion:   "dev",
		SchemaVersion: report.SchemaVersion,
		Ts:            time.Now(),
	})

	ctx, cancel := signalAwareContext()
	defer cancel()

	runStart := time.Now()
	var totalPass, totalFail int

	for _, plan := range plans {
		fixtureStart := emitFixtureStart(rep, plan)
		fix, err := startFixture(ctx, cfg.SourceDir, plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "suite %q: %v\n", plan.Suite.Name, err)
			return 2
		}
		emitFixtureReady(rep, plan, fixtureStart)
		ep, err := resolveEndpoint(cfg.SourceDir, plan, *gcxContextOverride, *appHost, *appPort, *otlpHTTP)
		if err != nil {
			if fix != nil {
				_ = closeFixture(rep, plan, fix)
			}
			fmt.Fprintf(os.Stderr, "suite %q: %v\n", plan.Suite.Name, err)
			return 2
		}

		rep.Emit(report.Event{
			Type:        report.EventSuiteStart,
			Suite:       plan.Suite.Name,
			Fixture:     plan.Suite.Fixture,
			FixtureType: plan.Fixture.Type,
			CaseCount:   len(plan.Cases),
		})

		gcxExec := &engine.GCX{Binary: *gcxBin, Context: ep.GCXContext}
		r := runner.New(gcxExec, rep, ep, runner.Options{
			Timeout:         *timeout,
			Interval:        *interval,
			AbsentTimeout:   *absentTimeout,
			SeedSettleDelay: *seedSettle,
		})
		if !*noCache && *cacheDir != "" {
			ttl := time.Duration(cfg.Cache.TTLDays) * 24 * time.Hour
			store, cacheErr := cache.New(*cacheDir, ttl, nil)
			if cacheErr != nil {
				fmt.Fprintln(os.Stderr, "cache disabled:", cacheErr)
			} else {
				fixtureBytes, _ := json.Marshal(plan.Fixture) // stable across calls
				r = r.WithCache(store, runner.CacheContext{
					GCXVersion:   gcxVersion(*gcxBin),
					OatsVersion:  "dev",
					FixtureBytes: fixtureBytes,
				})
			}
		}

		var suitePass, suiteFail int
		for _, c := range plan.Cases {
			if ctx.Err() != nil {
				break
			}
			if r.RunCase(ctx, c) {
				suitePass++
			} else {
				suiteFail++
			}
		}
		totalPass += suitePass
		totalFail += suiteFail

		rep.Emit(report.Event{
			Type:  report.EventSuiteEnd,
			Suite: plan.Suite.Name,
			Pass:  suitePass,
			Fail:  suiteFail,
		})
		if fix != nil {
			if closeErr := closeFixture(rep, plan, fix); closeErr != nil {
				fmt.Fprintf(os.Stderr, "suite %q: fixture shutdown: %v\n", plan.Suite.Name, closeErr)
				return 2
			}
		}
	}

	rep.Emit(report.Event{
		Type:       report.EventRunEnd,
		Pass:       totalPass,
		Fail:       totalFail,
		DurationMs: time.Since(runStart).Milliseconds(),
	})

	if totalFail > 0 || ctx.Err() != nil {
		return 1
	}
	return 0
}

func newReporter(w *os.File, format string, v report.Verbosity) report.Reporter {
	switch strings.ToLower(format) {
	case "ndjson", "json":
		return report.NewNDJSONReporter(w, v)
	default:
		return report.NewTextReporter(w, v)
	}
}

func verbosityFromInt(n int) report.Verbosity {
	switch {
	case n <= 0:
		return report.VerboseDefault
	case n == 1:
		return report.VerbosePasses
	case n == 2:
		return report.VerboseCmd
	default:
		return report.VerboseAll
	}
}

// resolveEndpoint maps a fixture config + an explicit override into the
// concrete endpoint the runner needs.
func resolveEndpoint(sourceDir string, plan discovery.Plan, gcxContextOverride, appHost string, appPort int, otlpHTTP string) (runner.Endpoint, error) {
	ep := runner.Endpoint{AppHost: appHost, AppPort: appPort, OTLPHTTP: otlpHTTP}
	switch plan.Fixture.Type {
	case "remote":
		// For a remote fixture, the gcx context is configured externally
		// (e.g. `gcx login` already ran). We pass the fixture name as a
		// best-effort default; --gcx-context overrides.
		ep.GCXContext = plan.Suite.Fixture
		ep.OTLPHTTP = plan.Fixture.Endpoint
	case "compose":
		ep.GCXContext = plan.Suite.Fixture
	case "k3d":
		ep.GCXContext = plan.Suite.Fixture
		if plan.Fixture.AppPort > 0 {
			ep.AppPort = plan.Fixture.AppPort
		}
	case "":
		// No fixture configured — caller (or --gcx-context) must supply
		// everything. Useful while plumbing the new CLI against an external setup.
	default:
		return ep, fmt.Errorf("fixture type %q is not supported in oats", plan.Fixture.Type)
	}
	if gcxContextOverride != "" {
		ep.GCXContext = gcxContextOverride
	}
	if ep.GCXContext == "" {
		return ep, fmt.Errorf("gcx context unresolved; set fixture.<name>.endpoint or pass --gcx-context")
	}
	return ep, nil
}

type suiteFixture interface {
	Close() error
}

type startableSuiteFixture interface {
	suiteFixture
	Up() error
}

func startFixture(_ context.Context, sourceDir string, plan discovery.Plan) (suiteFixture, error) {
	switch plan.Fixture.Type {
	case "", "remote":
		return nil, nil
	case "compose":
		composeFiles, err := resolveComposeFiles(sourceDir, plan.Fixture)
		if err != nil {
			return nil, err
		}
		suite, err := newComposeSuite(composeFiles, plan.Fixture.Env)
		if err != nil {
			return nil, err
		}
		if err := startSuiteFixture(suite); err != nil {
			return nil, err
		}
		return suite, nil
	case "k3d":
		ep := newKubernetesEndpoint(sourceDir, plan)
		if err := ep.Start(context.Background()); err != nil {
			return nil, err
		}
		return endpointFixture{ep: ep}, nil
	default:
		return nil, fmt.Errorf("fixture type %q is not supported in oats", plan.Fixture.Type)
	}
}

func resolveComposeFiles(sourceDir string, fixture discovery.FixtureConfig) ([]string, error) {
	var files []string
	switch {
	case fixture.ComposeFile != "":
		files = append(files, filepath.Join(sourceDir, fixture.ComposeFile))
	case len(fixture.ComposeFiles) > 0:
		for _, file := range fixture.ComposeFiles {
			files = append(files, filepath.Join(sourceDir, file))
		}
	case fixture.Template == "lgtm":
		files = append(files, filepath.Join(sourceDir, "docker-compose.yml"))
	case fixture.Template == "":
		return nil, fmt.Errorf("compose fixture requires compose_file, compose_files, or supported template")
	default:
		return nil, fmt.Errorf("unsupported compose fixture template %q", fixture.Template)
	}
	return files, nil
}

func startSuiteFixture(fix suiteFixture) error {
	startable, ok := fix.(startableSuiteFixture)
	if !ok {
		return fmt.Errorf("fixture does not support startup")
	}
	return startable.Up()
}

func emitFixtureStart(rep report.Reporter, plan discovery.Plan) time.Time {
	start := time.Now()
	if plan.Fixture.Type != "" && plan.Fixture.Type != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureStart,
			Suite:       plan.Suite.Name,
			Fixture:     plan.Suite.Fixture,
			FixtureType: plan.Fixture.Type,
			Ts:          start,
		})
	}
	return start
}

func emitFixtureReady(rep report.Reporter, plan discovery.Plan, start time.Time) {
	if plan.Fixture.Type != "" && plan.Fixture.Type != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureReady,
			Suite:       plan.Suite.Name,
			Fixture:     plan.Suite.Fixture,
			FixtureType: plan.Fixture.Type,
			DurationMs:  time.Since(start).Milliseconds(),
		})
	}
}

func closeFixture(rep report.Reporter, plan discovery.Plan, fix suiteFixture) error {
	start := time.Now()
	if err := fix.Close(); err != nil {
		return err
	}
	if plan.Fixture.Type != "" && plan.Fixture.Type != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureTeardown,
			Suite:       plan.Suite.Name,
			Fixture:     plan.Suite.Fixture,
			FixtureType: plan.Fixture.Type,
			DurationMs:  time.Since(start).Milliseconds(),
		})
	}
	return nil
}

type endpointFixture struct {
	ep *remote.Endpoint
}

func (e endpointFixture) Close() error {
	return e.ep.Stop(context.Background())
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// gcxVersion calls "gcx --version" once and returns the first line of
// output, or "" if gcx is unreachable. The version contributes to the
// cache key so an upgrade to gcx invalidates all green records.
func gcxVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// defaultCacheDir returns $XDG_STATE_HOME/oats or ~/.cache/oats. The cache
// lives under XDG_STATE_HOME on purpose (it is regeneratable state, not
// configuration). Falls back to a relative path if HOME is unset.
func defaultCacheDir() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "oats")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".cache", "oats")
	}
	return ".oats-cache"
}

func signalAwareContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()
	return ctx, cancel
}
