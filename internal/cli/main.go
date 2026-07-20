// Command oats is the OATS binary entry point.
//
// This package contains the gcx-driven CLI implementation used by the root
// oats binary.
//
// Usage:
//
//	oats [paths...]        run the cases (implicit; same as `oats run`)
//	oats run [paths...]    run the cases; positional paths scope which cases run
//	oats list              print the run plan and exit
//	oats migrate <path>    migrate a legacy file (stdout) or directory (in place)
//	oats cache clear       delete all cached results
//	oats version           print the version
//
// The config (oats-config.yaml) is found in the current directory or any parent
// unless --config is given. Positional paths (e.g. `oats examples/`) restrict the
// run to cases at or under them.
//
// Run flags (subset):
//
//	--config       Path to oats-config.yaml (default: found from cwd upward)
//	--gcx          Path to gcx binary (default "gcx" on PATH)
//	--gcx-version  Download and use a specific gcx release
//	--format       Output format: "text" (default) or "ndjson"
//	--tags         Comma-separated tag any-match filter (on case tags)
//	--fail-fast    Stop scheduling further cases after the first failure
//	-v / -vv / -vvv  Progressive verbosity (passes / commands / lifecycle)
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/fixture"
	"github.com/grafana/oats/migrate"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
	"github.com/grafana/oats/testhelpers"
	"github.com/grafana/oats/testhelpers/container"
)

// Version is the oats CLI version. Release builds can override this with
// -ldflags "-X github.com/grafana/oats/internal/cli.Version=vX.Y.Z".
var Version = "dev"

// DefaultGCXVersion is the gcx version pinned by the build. Release and mise
// builds inject it with -ldflags so oats can provide a reproducible fallback
// when gcx is not already available on PATH.
var DefaultGCXVersion = ""

type suiteResult struct {
	pass int
	fail int
	err  error
}

func Main() {
	os.Exit(Run())
}

// Run builds the cobra command tree and executes it. The exit code is
// threaded through *exit so a run with failing cases returns 1 without cobra
// treating it as a usage error.
func Run() int {
	var exit int
	root := newRootCmd(&exit)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if exit == 0 {
			exit = 2
		}
	}
	return exit
}

func newRootCmd(exit *int) *cobra.Command {
	var verbose int
	root := &cobra.Command{
		Use:   "oats [paths...]",
		Short: "OpenTelemetry Acceptance Tests — the gcx-driven runner",
		Long: "OpenTelemetry Acceptance Tests — the gcx-driven runner.\n\n" +
			"With no subcommand, oats runs the cases in oats-config.yaml (found in the\n" +
			"current directory or any parent). Optional positional paths scope the run to\n" +
			"cases at or under them, e.g. `oats examples/` or `oats examples/go/oats-case.yaml`.",
		// Do not print full command usage for runtime failures such as failed
		// assertions or unreachable backends. Those are not CLI syntax errors.
		SilenceUsage: true,
		// Let Run() own error printing and exit-code mapping. Without this, Cobra
		// prints returned errors too, producing duplicate "Error: ..." lines.
		SilenceErrors: true,
		// Bare `oats [paths...] [flags]` is an implicit `run`.
		// Cobra otherwise treats the first path as an unknown subcommand when
		// this command also has named subcommands.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAction(cmd, args, verbose, exit)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return applyEnvFlags(cmd.Flags())
		},
	}
	root.PersistentFlags().CountVarP(&verbose, "verbose", "v", "increase verbosity (-v, -vv, -vvv)")
	addRunFlags(root.Flags())

	root.AddCommand(
		newRunCmd(&verbose, exit),
		newListCmd(),
		newMigrateCmd(),
		newCacheCmd(),
		newVersionCmd(),
	)
	return root
}

// addRunFlags registers the flags shared by the implicit-run root and the
// explicit `run` subcommand.
func addRunFlags(fs *pflag.FlagSet) {
	fs.String("config", "oats-config.yaml", "path to oats-config.yaml")
	fs.String("gcx", "gcx", "path to gcx binary (PATH-resolved if a bare name)")
	fs.String("gcx-version", "", "download and use this gcx release (for example, 0.4.3)")
	fs.String("gcx-download", defaultGCXDownloadPolicy(), "gcx fallback download policy: auto | never")
	fs.String("format", "text", "output format: text | ndjson")
	fs.String("tags", "", "comma-separated tag any-match")
	fs.Duration("timeout", 30*time.Second, "per-assertion timeout")
	fs.Duration("interval", 500*time.Millisecond, "polling interval")
	fs.Duration("absent-timeout", 10*time.Second, "how long an absent assertion must stay absent")
	fs.Duration("seed-settle", 2*time.Second, "post-seed wait before first assertion")
	fs.String("gcx-context", "", "override the gcx --context value (otherwise derived from fixture endpoint)")
	fs.String("container-runtime", "auto", "container engine for Compose fixtures: auto | docker | podman")
	fs.String("app-host", "localhost", "application host for driving case input requests")
	fs.Int("app-port", 8080, "application port for driving case input requests")
	fs.String("otlp-http", defaultOTLPHTTP(), "OTLP/HTTP base URL for inline-otlp seed mode")
	fs.Int("parallel", 1, "number of fixture groups to run in parallel when fixture isolation allows it")
	fs.Bool("fail-fast", false, "stop scheduling further cases after the first case failure")
	fs.Bool("no-cache", false, "disable the skip-when-unchanged cache for this run")
	fs.String("cache-dir", defaultCacheDir(), "directory for the skip-when-unchanged cache")

	// Deprecated flag aliases, superseded by the `list` and `migrate`
	// subcommands. Hidden but still honored so existing invocations keep working.
	fs.Bool("list", false, "deprecated: use `oats list`")
	fs.String("migrate", "", "deprecated: use `oats migrate <file>`")
	mustMarkHidden(fs, "list")
	mustMarkHidden(fs, "migrate")
}

func mustMarkHidden(fs *pflag.FlagSet, name string) {
	if err := fs.MarkHidden(name); err != nil {
		panic(err)
	}
}

func newRunCmd(verbose, exit *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [paths...]",
		Short: "Run the cases in oats-config.yaml (default when no subcommand is given)",
		Long: "Run the cases declared by oats-config.yaml.\n\n" +
			"The config is found in the current directory or any parent (override with\n" +
			"--config). Optional positional paths scope the run to cases at or under those\n" +
			"files/directories, e.g. `oats run examples/` runs only the cases under examples/.",
		// Runtime failures should print the concise error/report, not usage.
		SilenceUsage: true,
		// Let Run() own error printing and exit-code mapping. Without this, Cobra
		// prints returned errors too, producing duplicate "Error: ..." lines.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAction(cmd, args, *verbose, exit)
		},
	}
	addRunFlags(cmd.Flags())
	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "Print the run plan and exit (no execution)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listAction(cmd)
		},
	}
	cmd.Flags().String("config", "oats-config.yaml", "path to oats-config.yaml")
	return cmd
}

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate <legacy-yaml | dir>",
		Short: "Convert legacy OATS yaml to the current shape",
		Long: `Convert legacy OATS test cases to the current shape.

File mode (argument is a file):
  Convert one legacy yaml and print the self-contained v3 case (including its
  fixture: block) to stdout. Warnings go to stderr.

Directory mode (argument is a directory):
  Migrate every legacy case found under the directory in place (each file is
  overwritten with its v3 equivalent) and write an oats-config.yaml listing
  them explicitly. A human summary and per-file warnings go to stderr; nothing
  is written to stdout.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return migrateAction(args[0])
		},
	}
	return cmd
}

func newCacheCmd() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the skip-when-unchanged cache",
	}
	clear := &cobra.Command{
		Use:           "clear",
		Short:         "Delete all cached results",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("cache-dir")
			store, err := cache.New(dir, 0, nil)
			if err != nil {
				return err
			}
			if err := store.Clear(); err != nil {
				return err
			}
			fmt.Println("cache cleared:", dir)
			return nil
		},
	}
	clear.Flags().String("cache-dir", defaultCacheDir(), "cache directory to clear")
	cacheCmd.AddCommand(clear)
	return cacheCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the oats version and exit",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(Version)
			return nil
		},
	}
}

func listAction(cmd *cobra.Command) error {
	configPath, err := resolveConfigPath(cmd.Flags())
	if err != nil {
		return err
	}
	cfg, err := discovery.Load(configPath)
	if err != nil {
		return err
	}
	plans, err := cfg.PlanRun(discovery.Filter{})
	if err != nil {
		return err
	}
	fmt.Print(discovery.Summary(plans))
	return nil
}

func migrateAction(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		res, err := migrate.ConvertTree(path)
		if err != nil {
			return err
		}
		for _, w := range res.Warnings {
			fmt.Fprintln(os.Stderr, "migrate warning:", w)
		}
		for _, f := range res.Written {
			fmt.Fprintln(os.Stderr, "migrated:", f)
		}
		fmt.Fprintf(os.Stderr, "wrote %d case(s); config: %s\n", len(res.Written), res.Config)
		return nil
	}

	out, warnings, err := migrate.ConvertFile(path)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "migrate warning:", w)
	}
	fmt.Print(string(out))
	return nil
}

// runAction is the shared implementation behind the implicit-run root and the
// explicit `run` subcommand. Positional args are file/dir paths that scope which
// cases run; the config is resolved from cwd (walking up), independent of them.
func runAction(cmd *cobra.Command, args []string, verbose int, exit *int) error {
	fs := cmd.Flags()

	// Honor the deprecated --list / --migrate flags.
	if list, _ := fs.GetBool("list"); list {
		return listAction(cmd)
	}
	if migratePath, _ := fs.GetString("migrate"); migratePath != "" {
		return migrateAction(migratePath)
	}

	configPath, err := resolveConfigPath(fs)
	if err != nil {
		return err
	}
	cfg, err := discovery.Load(configPath)
	if err != nil {
		return err
	}

	paths, err := absArgs(args)
	if err != nil {
		return err
	}
	filter := discovery.Filter{
		Tags:  splitCSV(flagStr(fs, "tags")),
		Paths: paths,
	}
	plans, err := cfg.PlanRun(filter)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		if len(paths) > 0 {
			return fmt.Errorf("no cases matched the given path(s)")
		}
		return fmt.Errorf("no cases matched the filter")
	}

	rep := newReporter(os.Stdout, flagStr(fs, "format"), verbosityFromInt(verbose))
	defer func() { _ = rep.Close() }()
	// Parallel fixture groups share one reporter. Serialize Emit/Close so text
	// lines and ndjson records are written one event at a time.
	rep = &lockedReporter{inner: rep}

	rep.Emit(report.Event{
		Type:          report.EventRunStart,
		OatsVersion:   Version,
		SchemaVersion: report.SchemaVersion,
		Ts:            time.Now(),
	})

	ctx, cancel := signalAwareContext()
	defer cancel()

	gcxBin := flagStr(fs, "gcx")
	containerRuntime := flagStr(fs, "container-runtime")
	if _, err := container.Parse(containerRuntime); err != nil {
		return err
	}
	if version := flagStr(fs, "gcx-version"); version != "" {
		if fs.Changed("gcx") {
			return fmt.Errorf("--gcx and --gcx-version cannot be used together")
		}
		gcxBin, err = bootstrapGCX(version, flagStr(fs, "cache-dir"))
		if err != nil {
			return err
		}
	} else {
		gcxBin, err = resolveDefaultGCX(fs, gcxBin)
		if err != nil {
			return err
		}
	}

	runStart := time.Now()
	opts := runOptions{
		gcxBin:             gcxBin,
		gcxContextOverride: flagStr(fs, "gcx-context"),
		containerRuntime:   containerRuntime,
		appHost:            flagStr(fs, "app-host"),
		appPort:            flagInt(fs, "app-port"),
		otlpHTTP:           flagStr(fs, "otlp-http"),
		timeout:            flagDur(fs, "timeout"),
		interval:           flagDur(fs, "interval"),
		absentTimeout:      flagDur(fs, "absent-timeout"),
		seedSettle:         flagDur(fs, "seed-settle"),
		noCache:            flagBool(fs, "no-cache"),
		cacheDir:           flagStr(fs, "cache-dir"),
		cacheTTLDays:       cfg.Cache.TTLDays,
		failFast:           flagBool(fs, "fail-fast"),
	}
	totalPass, totalFail, runErr := runPlans(ctx, rep, plans, opts, flagInt(fs, "parallel"))
	if runErr != nil {
		return runErr
	}

	rep.Emit(report.Event{
		Type:       report.EventRunEnd,
		Pass:       totalPass,
		Fail:       totalFail,
		DurationMs: time.Since(runStart).Milliseconds(),
	})

	if totalFail > 0 || ctx.Err() != nil {
		*exit = 1
	}
	return nil
}

func flagStr(fs *pflag.FlagSet, name string) string { v, _ := fs.GetString(name); return v }
func flagInt(fs *pflag.FlagSet, name string) int    { v, _ := fs.GetInt(name); return v }
func flagBool(fs *pflag.FlagSet, name string) bool  { v, _ := fs.GetBool(name); return v }
func flagDur(fs *pflag.FlagSet, name string) (d time.Duration) {
	d, _ = fs.GetDuration(name)
	return d
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
func resolveEndpoint(plan discovery.Plan, rt fixture.Runtime, gcxContextOverride, appHost string, appPort int, otlpHTTP string) (runner.Endpoint, error) {
	ep := runner.Endpoint{AppHost: appHost, AppPort: appPort, OTLPHTTP: otlpHTTP}
	switch plan.Fixture.Kind() {
	case "remote":
		// For a remote fixture, the gcx context is configured externally
		// (e.g. `gcx login` already ran). We pass the suite name as a
		// best-effort default; --gcx-context overrides.
		ep.GCXContext = plan.Name
		if gcxContextOverride != "" {
			ep.GCXContext = gcxContextOverride
		}
		ep.OTLPHTTP = plan.Fixture.Remote.Endpoint
		grafana, err := remoteGrafanaURL(ep.GCXContext)
		if err != nil {
			return runner.Endpoint{}, fmt.Errorf("resolve remote Grafana URL: %w", err)
		}
		ep.CustomCheckEnv = append(ep.CustomCheckEnv, "OATS_FIXTURE_TYPE=remote")
		if grafana != "" {
			ep.CustomCheckEnv = append(ep.CustomCheckEnv, "OATS_GRAFANA_URL="+grafana)
		}
		ep.CustomCheckEnv = append(ep.CustomCheckEnv, "OATS_APP_URL="+fmt.Sprintf("http://%s:%d", ep.AppHost, ep.AppPort))
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
		ep.CustomCheckEnv = append(ep.CustomCheckEnv, rt.CustomCheckEnv...)
	default:
		return ep, fmt.Errorf("fixture kind %q is not supported in oats", plan.Fixture.Kind())
	}
	// A fixture that resolved a concrete app host port drives the app there —
	// compose discovers the published ephemeral port, k3d uses the configured
	// one. Surfaced uniformly through Runtime so every fixture type reads it the
	// same way.
	if rt.AppHostPort > 0 {
		ep.AppPort = rt.AppHostPort
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
	containerRuntime   string
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
	failFast           bool
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
		if safe, _ := fixture.SupportsParallel(plan); safe {
			parallelPlans = append(parallelPlans, plan)
		} else {
			serialPlans = append(serialPlans, plan)
		}
	}

	totalPass, totalFail, err := runPlansParallel(ctx, rep, parallelPlans, opts, parallel)
	if err != nil {
		return totalPass, totalFail, err
	}
	if opts.failFast && totalFail > 0 {
		return totalPass, totalFail, nil
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
		if opts.failFast && totalFail > 0 {
			break
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
				if res.err != nil || (opts.failFast && res.fail > 0) {
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
	fix, rt, err := fixture.StartWithOptions(ctx, plan, fixture.Options{ContainerRuntime: opts.containerRuntime})
	if err != nil {
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Name, err)}
	}
	if err := fixture.WaitForReady(plan, rt); err != nil {
		if fix != nil {
			_ = closeFixture(rep, plan, fix)
		}
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Name, err)}
	}
	emitFixtureReady(rep, plan, fixtureStart)
	ep, err := resolveEndpoint(plan, rt, opts.gcxContextOverride, opts.appHost, opts.appPort, opts.otlpHTTP)
	if err != nil {
		if fix != nil {
			_ = closeFixture(rep, plan, fix)
		}
		return suiteResult{err: fmt.Errorf("suite %q: %w", plan.Name, err)}
	}

	rep.Emit(report.Event{
		Type:        report.EventSuiteStart,
		Suite:       plan.Name,
		FixtureType: plan.Fixture.Kind(),
		CaseCount:   len(plan.Cases),
	})

	gcxExec := &engine.GCX{Binary: opts.gcxBin, Context: ep.GCXContext, Config: ep.GCXConfig, Env: ep.GCXEnv}
	r := runner.New(gcxExec, rep, ep, runner.Options{
		OatsVersion:     Version,
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
			if opts.failFast {
				break
			}
		}
	}

	rep.Emit(report.Event{
		Type:  report.EventSuiteEnd,
		Suite: plan.Name,
		Pass:  suitePass,
		Fail:  suiteFail,
	})
	if fix != nil {
		if closeErr := closeFixture(rep, plan, fix); closeErr != nil {
			return suiteResult{pass: suitePass, fail: suiteFail, err: fmt.Errorf("suite %q: fixture shutdown: %w", plan.Name, closeErr)}
		}
	}
	return suiteResult{pass: suitePass, fail: suiteFail}
}

func emitFixtureStart(rep report.Reporter, plan discovery.Plan) time.Time {
	start := time.Now()
	if plan.Fixture.Kind() != "" && plan.Fixture.Kind() != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureStart,
			Suite:       plan.Name,
			FixtureType: plan.Fixture.Kind(),
			Ts:          start,
		})
	}
	return start
}

func emitFixtureReady(rep report.Reporter, plan discovery.Plan, start time.Time) {
	if plan.Fixture.Kind() != "" && plan.Fixture.Kind() != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureReady,
			Suite:       plan.Name,
			FixtureType: plan.Fixture.Kind(),
			DurationMs:  time.Since(start).Milliseconds(),
		})
	}
}

func closeFixture(rep report.Reporter, plan discovery.Plan, fix fixture.Handle) error {
	start := time.Now()
	if err := fix.Close(); err != nil {
		return err
	}
	if plan.Fixture.Kind() != "" && plan.Fixture.Kind() != "remote" {
		rep.Emit(report.Event{
			Type:        report.EventFixtureTeardown,
			Suite:       plan.Name,
			FixtureType: plan.Fixture.Kind(),
			DurationMs:  time.Since(start).Milliseconds(),
		})
	}
	return nil
}

func defaultOTLPHTTP() string {
	return fmt.Sprintf("http://%s:%d", testhelpers.LocalhostIPv4, testhelpers.OTLPHTTPPort)
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

// resolveConfigPath returns the oats-config.yaml to load. An explicit --config
// is honored as-is; otherwise the default filename is searched for in the
// current directory and each parent (so `oats` works from a subdirectory).
func resolveConfigPath(fs *pflag.FlagSet) (string, error) {
	name, _ := fs.GetString("config")
	if fs.Changed("config") {
		return name, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; {
		candidate := filepath.Join(dir, name)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent directory (use --config)", name, cwd)
		}
		dir = parent
	}
}

// absArgs cleans positional path args to absolute paths (relative to cwd) for
// case-path scoping.
func absArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.Clean(abs))
	}
	return out, nil
}

// defaultCacheDir returns a per-user cache location using platform-aware Go
// defaults. On Unix, XDG_STATE_HOME wins when set because the cache is
// regeneratable state; otherwise os.UserCacheDir selects the right location for
// Unix, macOS, and Windows.
func defaultCacheDir() string {
	if runtime.GOOS != "windows" {
		if s := os.Getenv("XDG_STATE_HOME"); s != "" {
			return filepath.Join(s, "oats")
		}
	}
	if s, err := os.UserCacheDir(); err == nil {
		return filepath.Join(s, "oats")
	}
	return ".oats-cache"
}

// signalAwareContext cancels on Ctrl+C everywhere, and also on SIGTERM on
// platforms that expose it. Windows does not need a SIGTERM special case.
func signalAwareContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	notifySignals := []os.Signal{os.Interrupt}
	if runtime.GOOS != "windows" {
		notifySignals = append(notifySignals, syscall.SIGTERM)
	}
	signal.Notify(sigs, notifySignals...)
	go func() {
		<-sigs
		signal.Stop(sigs)
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
