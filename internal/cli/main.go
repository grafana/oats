// Command oats is the OATS binary entry point.
//
// This package contains the gcx-driven CLI implementation used by the root
// oats binary.
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
//	-v=1 / -v=2 / -v=3  Progressive verbosity (passes / commands / lifecycle)
//	--suite        Comma-separated suite names to include
//	--tags         Comma-separated tag any-match filter
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	// Version is the oats CLI version. Release builds can override this with
	// -ldflags "-X github.com/grafana/oats/internal/cli.Version=vX.Y.Z".
	Version = "dev"

	newComposeSuite = func(files []string, env []string) (suiteFixture, error) {
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

type fixtureRuntime struct {
	GrafanaURL       string
	OTLPHTTP         string
	PyroscopeURL     string
	CustomCheckEnv   []string
	ComposeFiles     []string
	ComposeProject   string
	GCXConfig        string
	ParallelSafe     bool
	ParallelDisabled string
}

type suiteResult struct {
	pass int
	fail int
	err  error
}

func Main() {
	os.Exit(Run())
}

func Run() int {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(Version)
		return 0
	}

	configPath := flag.String("config", "oats.toml", "path to oats.toml")
	gcxBin := flag.String("gcx", "gcx", "path to gcx binary (PATH-resolved if a bare name)")
	listOnly := flag.Bool("list", false, "print the run plan and exit (no execution)")
	versionOnly := flag.Bool("version", false, "print the oats version and exit")
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
	parallel := flag.Int("parallel", 1, "number of suites to run in parallel when fixture isolation allows it")
	noCache := flag.Bool("no-cache", false, "disable the skip-when-unchanged cache for this run")
	cacheDir := flag.String("cache-dir", defaultCacheDir(), "directory for the skip-when-unchanged cache")

	var verbose int
	flag.IntVar(&verbose, "v", 0, "verbosity (0-3)")

	flag.Parse()

	if *versionOnly {
		fmt.Println(Version)
		return 0
	}

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
	rep = &lockedReporter{inner: rep}

	rep.Emit(report.Event{
		Type:          report.EventRunStart,
		OatsVersion:   Version,
		SchemaVersion: report.SchemaVersion,
		Ts:            time.Now(),
	})

	ctx, cancel := signalAwareContext()
	defer cancel()

	runStart := time.Now()
	opts := runOptions{
		gcxBin:             *gcxBin,
		gcxContextOverride: *gcxContextOverride,
		appHost:            *appHost,
		appPort:            *appPort,
		otlpHTTP:           *otlpHTTP,
		timeout:            *timeout,
		interval:           *interval,
		absentTimeout:      *absentTimeout,
		seedSettle:         *seedSettle,
		noCache:            *noCache,
		cacheDir:           *cacheDir,
		cacheTTLDays:       cfg.Cache.TTLDays,
	}
	totalPass, totalFail, runErr := runPlans(ctx, rep, plans, opts, *parallel)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		return 2
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
func resolveEndpoint(plan discovery.Plan, rt fixtureRuntime, gcxContextOverride, appHost string, appPort int, otlpHTTP string) (runner.Endpoint, error) {
	ep := runner.Endpoint{AppHost: appHost, AppPort: appPort, OTLPHTTP: otlpHTTP}
	switch plan.Fixture.Type {
	case "remote":
		// For a remote fixture, the gcx context is configured externally
		// (e.g. `gcx login` already ran). We pass the fixture name as a
		// best-effort default; --gcx-context overrides.
		ep.GCXContext = plan.Suite.Fixture
		ep.OTLPHTTP = plan.Fixture.Endpoint
		ep.CustomCheckEnv = append(ep.CustomCheckEnv,
			"OATS_FIXTURE_TYPE=remote",
			"OATS_GRAFANA_URL="+grafanaURL(),
			"OATS_APP_URL="+fmt.Sprintf("http://%s:%d", ep.AppHost, ep.AppPort),
		)
	case "compose":
		if rt.GCXConfig != "" {
			ep.GCXConfig = rt.GCXConfig
		}
		if rt.OTLPHTTP != "" {
			ep.OTLPHTTP = rt.OTLPHTTP
		}
		ep.CustomCheckEnv = append(ep.CustomCheckEnv, rt.CustomCheckEnv...)
	case "k3d":
		if rt.GCXConfig != "" {
			ep.GCXConfig = rt.GCXConfig
		}
		if rt.OTLPHTTP != "" {
			ep.OTLPHTTP = rt.OTLPHTTP
		}
		if plan.Fixture.AppPort > 0 {
			ep.AppPort = plan.Fixture.AppPort
		}
		ep.CustomCheckEnv = append(ep.CustomCheckEnv, rt.CustomCheckEnv...)
	case "":
		// No fixture configured — caller (or --gcx-context) must supply
		// everything. Useful while plumbing the new CLI against an external setup.
		ep.CustomCheckEnv = append(ep.CustomCheckEnv,
			"OATS_APP_URL="+fmt.Sprintf("http://%s:%d", ep.AppHost, ep.AppPort),
		)
	default:
		return ep, fmt.Errorf("fixture type %q is not supported in oats", plan.Fixture.Type)
	}
	if gcxContextOverride != "" {
		ep.GCXContext = gcxContextOverride
	}
	if ep.GCXContext == "" && ep.GCXConfig == "" && len(ep.GCXEnv) == 0 {
		return ep, fmt.Errorf("gcx context unresolved; set fixture.<name>.endpoint or pass --gcx-context")
	}
	return ep, nil
}

type runOptions struct {
	gcxBin             string
	gcxContextOverride string
	appHost            string
	appPort            int
	otlpHTTP           string
	timeout            time.Duration
	interval           time.Duration
	absentTimeout      time.Duration
	seedSettle         time.Duration
	noCache            bool
	cacheDir           string
	cacheTTLDays       int
}

func runPlans(ctx context.Context, rep report.Reporter, plans []discovery.Plan, opts runOptions, parallel int) (int, int, error) {
	if parallel < 1 {
		parallel = 1
	}
	if parallel == 1 || len(plans) <= 1 {
		return runPlansSequential(ctx, rep, plans, opts)
	}

	var serialPlans, parallelPlans []discovery.Plan
	for _, plan := range plans {
		if safe, _ := planSupportsParallel(plan); safe {
			parallelPlans = append(parallelPlans, plan)
		} else {
			serialPlans = append(serialPlans, plan)
		}
	}

	totalPass, totalFail, err := runPlansParallel(ctx, rep, parallelPlans, opts, parallel)
	if err != nil {
		return totalPass, totalFail, err
	}

	pass, fail, err := runPlansSequential(ctx, rep, serialPlans, opts)
	return totalPass + pass, totalFail + fail, err
}

func runPlansSequential(ctx context.Context, rep report.Reporter, plans []discovery.Plan, opts runOptions) (int, int, error) {
	var totalPass, totalFail int
	for _, plan := range plans {
		res := runPlan(ctx, rep, plan, opts)
		totalPass += res.pass
		totalFail += res.fail
		if res.err != nil {
			return totalPass, totalFail, res.err
		}
	}
	return totalPass, totalFail, nil
}

func runPlansParallel(parent context.Context, rep report.Reporter, plans []discovery.Plan, opts runOptions, parallel int) (int, int, error) {
	if len(plans) == 0 {
		return 0, 0, nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	workCh := make(chan discovery.Plan)
	resultCh := make(chan suiteResult, len(plans))
	workers := parallel
	if workers > len(plans) {
		workers = len(plans)
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for plan := range workCh {
				res := runPlan(ctx, rep, plan, opts)
				if res.err != nil {
					cancel()
				}
				resultCh <- res
			}
		}()
	}

	go func() {
		defer close(workCh)
		for _, plan := range plans {
			select {
			case <-ctx.Done():
				return
			case workCh <- plan:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var totalPass, totalFail int
	var firstErr error
	for res := range resultCh {
		totalPass += res.pass
		totalFail += res.fail
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}
	return totalPass, totalFail, firstErr
}

func runPlan(ctx context.Context, rep report.Reporter, plan discovery.Plan, opts runOptions) suiteResult {
	fixtureStart := emitFixtureStart(rep, plan)
	fix, rt, err := startFixture(ctx, plan)
	if err != nil {
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Suite.Name, err)}
	}
	if err := waitForFixtureReady(plan, rt); err != nil {
		if fix != nil {
			_ = closeFixture(rep, plan, fix)
		}
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Suite.Name, err)}
	}
	emitFixtureReady(rep, plan, fixtureStart)
	ep, err := resolveEndpoint(plan, rt, opts.gcxContextOverride, opts.appHost, opts.appPort, opts.otlpHTTP)
	if err != nil {
		if fix != nil {
			_ = closeFixture(rep, plan, fix)
		}
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Suite.Name, err)}
	}

	rep.Emit(report.Event{
		Type:        report.EventSuiteStart,
		Suite:       plan.Suite.Name,
		Fixture:     plan.Suite.Fixture,
		FixtureType: plan.Fixture.Type,
		CaseCount:   len(plan.Cases),
	})

	gcxExec := &engine.GCX{Binary: opts.gcxBin, Context: ep.GCXContext, Config: ep.GCXConfig, Env: ep.GCXEnv}
	r := runner.New(gcxExec, rep, ep, runner.Options{
		Timeout:         opts.timeout,
		Interval:        opts.interval,
		AbsentTimeout:   opts.absentTimeout,
		SeedSettleDelay: opts.seedSettle,
	})
	if !opts.noCache && opts.cacheDir != "" {
		ttl := time.Duration(opts.cacheTTLDays) * 24 * time.Hour
		store, cacheErr := cache.New(opts.cacheDir, ttl, nil)
		if cacheErr != nil {
			fmt.Fprintln(os.Stderr, "cache disabled:", cacheErr)
		} else {
			fixtureBytes, _ := json.Marshal(plan.Fixture)
			r = r.WithCache(store, runner.CacheContext{
				GCXVersion:   gcxVersion(opts.gcxBin),
				OatsVersion:  Version,
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

	rep.Emit(report.Event{
		Type:  report.EventSuiteEnd,
		Suite: plan.Suite.Name,
		Pass:  suitePass,
		Fail:  suiteFail,
	})
	if fix != nil {
		if closeErr := closeFixture(rep, plan, fix); closeErr != nil {
			return suiteResult{pass: suitePass, fail: suiteFail, err: fmt.Errorf("suite %q: fixture shutdown: %w", plan.Suite.Name, closeErr)}
		}
	}
	return suiteResult{pass: suitePass, fail: suiteFail}
}

type suiteFixture interface {
	Close() error
}

type startableSuiteFixture interface {
	suiteFixture
	Up() error
}

func startFixture(ctx context.Context, plan discovery.Plan) (suiteFixture, fixtureRuntime, error) {
	switch plan.Fixture.Type {
	case "", "remote":
		return nil, fixtureRuntime{ParallelSafe: true}, nil
	case "compose":
		composeFiles, cleanup, err := resolveComposeFiles(plan.FixtureSourceDir, plan.Fixture)
		if err != nil {
			return nil, fixtureRuntime{}, err
		}
		project := composeProjectName(plan)
		suiteEnv := append([]string(nil), plan.Fixture.Env...)
		suiteEnv = append(suiteEnv, "COMPOSE_PROJECT_NAME="+project)
		suite, err := newComposeSuite(composeFiles, suiteEnv)
		if err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, err
		}
		if err := startSuiteFixture(suite); err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, err
		}
		grafanaPort, err := lookupComposePort(composeFiles, suiteEnv, "lgtm", "3000")
		if err != nil {
			_ = suite.Close()
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, err
		}
		otlpPort, err := lookupComposePort(composeFiles, suiteEnv, "lgtm", "4318")
		if err != nil {
			_ = suite.Close()
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, err
		}
		pyroscopePort, err := lookupComposePort(composeFiles, suiteEnv, "lgtm", "4040")
		if err != nil {
			_ = suite.Close()
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, err
		}
		rt := fixtureRuntime{
			GrafanaURL:     "http://127.0.0.1:" + grafanaPort,
			OTLPHTTP:       "http://127.0.0.1:" + otlpPort,
			PyroscopeURL:   "http://127.0.0.1:" + pyroscopePort,
			ComposeFiles:   composeFiles,
			ComposeProject: project,
		}
		cfg, cfgErr := writeLocalGCXConfig(rt.GrafanaURL)
		if cfgErr != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, fixtureRuntime{}, fmt.Errorf("write local gcx config: %w", cfgErr)
		}
		rt.GCXConfig = cfg
		cleanup = chainCleanup(func() error { return removeIfExists(cfg) }, cleanup)
		rt.CustomCheckEnv = composeCheckEnv(plan, rt)
		rt.ParallelSafe, rt.ParallelDisabled = planSupportsParallel(plan)
		return composeFixture{suite: suite, cleanup: cleanup}, rt, nil
	case "k3d":
		ports, err := allocateK3DPorts()
		if err != nil {
			return nil, fixtureRuntime{}, err
		}
		ep := newKubernetesEndpoint(plan, ports)
		if err := ep.Start(ctx); err != nil {
			return nil, fixtureRuntime{}, err
		}
		rt := fixtureRuntime{
			GrafanaURL:       fmt.Sprintf("http://localhost:%d", ports.GrafanaHTTPPort),
			OTLPHTTP:         fmt.Sprintf("http://localhost:%d", ports.OTLPHTTPPort),
			PyroscopeURL:     fmt.Sprintf("http://localhost:%d", ports.PyroscopeHttpPort),
			CustomCheckEnv:   k3dCheckEnv(runner.Endpoint{AppHost: "localhost", AppPort: plan.Fixture.AppPort}, ports),
			ParallelSafe:     false,
			ParallelDisabled: "k3d fixtures currently use shared clusters and kubectl port-forwards",
		}
		cfg, cfgErr := writeLocalGCXConfig(rt.GrafanaURL)
		if cfgErr != nil {
			_ = ep.Stop(context.Background())
			return nil, fixtureRuntime{}, fmt.Errorf("write local gcx config: %w", cfgErr)
		}
		rt.GCXConfig = cfg
		return endpointFixture{ep: ep, cleanup: func() error { return removeIfExists(cfg) }}, rt, nil
	default:
		return nil, fixtureRuntime{}, fmt.Errorf("fixture type %q is not supported in oats", plan.Fixture.Type)
	}
}

func resolveComposeFiles(sourceDir string, fixture discovery.FixtureConfig) ([]string, func() error, error) {
	var files []string
	var cleanup func() error
	if fixture.Template == "lgtm" {
		f, err := writeBuiltinLGTMCompose(sourceDir)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, f)
		cleanup = func() error { return os.Remove(f) }
	} else if fixture.Template != "" {
		return nil, nil, fmt.Errorf("unsupported compose fixture template %q", fixture.Template)
	}
	switch {
	case fixture.ComposeFile != "":
		files = append(files, filepath.Join(sourceDir, fixture.ComposeFile))
	case len(fixture.ComposeFiles) > 0:
		for _, file := range fixture.ComposeFiles {
			files = append(files, filepath.Join(sourceDir, file))
		}
	case fixture.Template == "":
		return nil, nil, fmt.Errorf("compose fixture requires compose_file, compose_files, or supported template")
	}
	return files, cleanup, nil
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
	suite   suiteFixture
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

func writeBuiltinLGTMCompose(sourceDir string) (string, error) {
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(sourceDir, ".oats.lgtm.*.compose.yml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	const body = `services:
  lgtm:
    image: ${LGTM_IMAGE:-docker.io/grafana/otel-lgtm:latest}
    ports:
      - "127.0.0.1::3000"
      - "127.0.0.1::4317"
      - "127.0.0.1::4318"
      - "127.0.0.1::3200"
      - "127.0.0.1::4040"
      - "127.0.0.1::9090"
`
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func grafanaURL() string { return "http://localhost:3000" }

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

func composeCheckEnv(plan discovery.Plan, rt fixtureRuntime) []string {
	files := rt.ComposeFiles
	if len(files) == 0 {
		return []string{"OATS_FIXTURE_TYPE=compose"}
	}
	return []string{
		"OATS_FIXTURE_TYPE=compose",
		"COMPOSE_PROJECT_NAME=" + rt.ComposeProject,
		"COMPOSE_FILE=" + strings.Join(files, string(os.PathListSeparator)),
		"OATS_COMPOSE_FILE_ARGS=" + composeFileArgs(files),
		"OATS_GRAFANA_URL=" + rt.GrafanaURL,
		"OATS_OTLP_HTTP=" + rt.OTLPHTTP,
		"OATS_PYROSCOPE_URL=" + rt.PyroscopeURL,
	}
}

func composeFileArgs(files []string) string {
	var parts []string
	for _, f := range files {
		parts = append(parts, "-f", shellQuote(f))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func k3dCheckEnv(ep runner.Endpoint, ports remote.PortsConfig) []string {
	return []string{
		"OATS_FIXTURE_TYPE=k3d",
		"OATS_APP_URL=" + fmt.Sprintf("http://%s:%d", ep.AppHost, ep.AppPort),
		"OATS_GRAFANA_URL=" + fmt.Sprintf("http://localhost:%d", ports.GrafanaHTTPPort),
		"OATS_OTLP_HTTP=" + fmt.Sprintf("http://localhost:%d", ports.OTLPHTTPPort),
		"OATS_PYROSCOPE_URL=" + fmt.Sprintf("http://localhost:%d", ports.PyroscopeHttpPort),
	}
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

func waitForFixtureReady(plan discovery.Plan, rt fixtureRuntime) error {
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

func planSupportsParallel(plan discovery.Plan) (bool, string) {
	switch plan.Fixture.Type {
	case "", "remote":
		return true, ""
	case "compose":
		if plan.Fixture.Template != "lgtm" {
			return false, "compose fixtures are only parallel-safe when OATS owns the LGTM ports via template=lgtm"
		}
		for _, c := range plan.Cases {
			if c.Seed.Type == "app" {
				return false, "compose suites with app seeds still rely on shared fixed app ports"
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

func allocateK3DPorts() (remote.PortsConfig, error) {
	grafanaPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	otlpHTTPPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	lokiPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	promPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	tempoPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	pyroscopePort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	return remote.PortsConfig{
		GrafanaHTTPPort:    grafanaPort,
		OTLPHTTPPort:       otlpHTTPPort,
		LokiHttpPort:       lokiPort,
		PrometheusHTTPPort: promPort,
		TempoHTTPPort:      tempoPort,
		PyroscopeHttpPort:  pyroscopePort,
	}, nil
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

func composeProjectName(plan discovery.Plan) string {
	name := strings.ToLower(plan.Suite.Name)
	if name == "" {
		name = "oats"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "oats"
	}
	if len(slug) > 32 {
		slug = slug[:32]
	}
	return fmt.Sprintf("oats-%s-%d", slug, time.Now().UnixNano())
}

func extraComposeFiles(plan discovery.Plan) []string {
	switch {
	case plan.Fixture.ComposeFile != "":
		return []string{filepath.Join(plan.FixtureSourceDir, plan.Fixture.ComposeFile)}
	case len(plan.Fixture.ComposeFiles) > 0:
		files := make([]string, 0, len(plan.Fixture.ComposeFiles))
		for _, file := range plan.Fixture.ComposeFiles {
			files = append(files, filepath.Join(plan.FixtureSourceDir, file))
		}
		return files
	default:
		return nil
	}
}

func composeFilePublishesFixedHostPorts(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	inPorts := false
	portsIndent := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if inPorts && indent <= portsIndent {
			inPorts = false
		}
		if strings.HasPrefix(trimmed, "ports:") {
			inPorts = true
			portsIndent = indent
			continue
		}
		if !inPorts {
			continue
		}
		if strings.Contains(trimmed, "published:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "published:"))
			if value != "" && value != "0" {
				return true, nil
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), `"'`)
		if value == "" {
			continue
		}
		if fixedShortPortMapping(value) {
			return true, nil
		}
	}
	return false, nil
}

func fixedShortPortMapping(value string) bool {
	if !strings.Contains(value, ":") {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		return false
	}
	hostPart := strings.Trim(parts[len(parts)-2], "[]")
	if _, err := strconv.Atoi(hostPart); err == nil && hostPart != "0" {
		return true
	}
	return false
}

func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec
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

func readComposeGrafanaToken(plan discovery.Plan) (string, error) {
	files, _, err := resolveComposeFiles(plan.FixtureSourceDir, plan.Fixture)
	if err != nil {
		return "", err
	}
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "exec", "-T", "lgtm", "sh", "-c", "cat /tmp/grafana-sa-token 2>/dev/null || true")
	cmd := exec.Command("docker", args...)
	cmd.Env = append(cmd.Environ(), plan.Fixture.Env...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func dockerComposePort(files []string, env []string, service, containerPort string) (string, error) {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "port", service, containerPort)
	cmd := exec.Command("docker", args...)
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	host, port, err := splitDockerHostPort(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("invalid docker compose port output %q", strings.TrimSpace(string(out)))
	}
	return port, nil
}

func splitDockerHostPort(addr string) (string, string, error) {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end < 0 || end+2 > len(addr) || addr[end+1] != ':' {
			return "", "", fmt.Errorf("invalid address %q", addr)
		}
		return addr[1:end], addr[end+2:], nil
	}
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid address %q", addr)
	}
	return addr[:idx], addr[idx+1:], nil
}

func readK3DGrafanaToken() (string, error) {
	cmd := exec.Command("kubectl", "exec", "deploy/lgtm", "--", "sh", "-c", "cat /tmp/grafana-sa-token 2>/dev/null || true")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
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

type lockedReporter struct {
	mu    sync.Mutex
	inner report.Reporter
}

func (r *lockedReporter) Emit(e report.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inner.Emit(e)
}

func (r *lockedReporter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inner.Close()
}
