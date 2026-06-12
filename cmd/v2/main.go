// Command oats-v2 is the new OATS binary entry point.
//
// While the v2 branch is in flight, this command lives alongside the v1
// "oats" binary at the repository root. The final v2 commit replaces the
// root main.go with the contents of this file; until then, both binaries
// coexist so v1 acceptance tests keep running unmodified.
//
// Usage:
//
//	oats-v2 [flags]
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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/engine"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int {
	configPath := flag.String("config", "oats.toml", "path to oats.toml")
	gcxBin := flag.String("gcx", "gcx", "path to gcx binary (PATH-resolved if a bare name)")
	listOnly := flag.Bool("list", false, "print the run plan and exit (no execution)")
	format := flag.String("format", "text", "output format: text | ndjson")
	suiteFilterStr := flag.String("suite", "", "comma-separated suite names")
	tagFilterStr := flag.String("tags", "", "comma-separated tag any-match")
	timeout := flag.Duration("timeout", 30*time.Second, "per-assertion timeout")
	interval := flag.Duration("interval", 500*time.Millisecond, "polling interval")
	absentTimeout := flag.Duration("absent-timeout", 10*time.Second, "absence-check window")
	seedSettle := flag.Duration("seed-settle", 2*time.Second, "post-seed wait before first assertion")
	gcxContextOverride := flag.String("gcx-context", "", "override the gcx --context value (otherwise derived from fixture endpoint)")

	var verbose int
	flag.IntVar(&verbose, "v", 0, "verbosity (0-3)")

	flag.Parse()

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
		OatsVersion:   "v2-dev",
		SchemaVersion: report.SchemaVersion,
		Ts:            time.Now(),
	})

	ctx, cancel := signalAwareContext()
	defer cancel()

	runStart := time.Now()
	var totalPass, totalFail int

	for _, plan := range plans {
		ep, err := resolveEndpoint(plan, *gcxContextOverride)
		if err != nil {
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

		exec := &engine.GCX{Binary: *gcxBin, Context: ep.GCXContext}
		r := runner.New(exec, rep, ep, runner.Options{
			Timeout:         *timeout,
			Interval:        *interval,
			AbsentTimeout:   *absentTimeout,
			SeedSettleDelay: *seedSettle,
		})

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
// concrete endpoint the runner needs. The v2 branch ships with "remote"
// support only at this stage; "compose" and "k3d" will land later.
func resolveEndpoint(plan discovery.Plan, gcxContextOverride string) (runner.Endpoint, error) {
	ep := runner.Endpoint{}
	switch plan.Fixture.Type {
	case "remote":
		// For a remote fixture, the gcx context is configured externally
		// (e.g. `gcx login` already ran). We pass the fixture name as a
		// best-effort default; --gcx-context overrides.
		ep.GCXContext = plan.Suite.Fixture
		ep.OTLPHTTP = plan.Fixture.Endpoint
	case "":
		// No fixture configured — caller (or --gcx-context) must supply
		// everything. Useful while plumbing v2 against an external setup.
	default:
		return ep, fmt.Errorf("fixture type %q is not yet supported in oats-v2 (compose/k3d arrive in follow-up commits)", plan.Fixture.Type)
	}
	if gcxContextOverride != "" {
		ep.GCXContext = gcxContextOverride
	}
	if ep.GCXContext == "" {
		return ep, fmt.Errorf("gcx context unresolved; set fixture.<name>.endpoint or pass --gcx-context")
	}
	return ep, nil
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
