package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/internal/cli/usagespec"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/runner"
)

func TestRootAndRunCommandsRegisterTheSameRunFlags(t *testing.T) {
	root, err := parseCommand(nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := parseCommand([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root.Run, run.Run) {
		t.Fatalf("implicit/explicit run defaults differ: %#v, %#v", root.Run, run.Run)
	}
}

func TestPauseOnFailurePrintsRedactedCoordinatesAndResumes(t *testing.T) {
	var output bytes.Buffer
	err := pauseOnFailure(
		context.Background(),
		discovery.Plan{Name: "local-lgtm", Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}}},
		&casefile.Case{Name: "missing trace"},
		runner.Endpoint{GCXConfig: "/tmp/oats-gcx-secret.yaml", GCXContext: "local"},
		runOptions{pauseInput: strings.NewReader("\n"), pauseOutput: &output},
	)
	if err != nil {
		t.Fatalf("pauseOnFailure: %v", err)
	}
	text := output.String()
	for _, want := range []string{"missing trace", "local-lgtm", "/tmp/oats-gcx-secret.yaml", "--context local"} {
		if !strings.Contains(text, want) {
			t.Errorf("pause output missing %q: %q", want, text)
		}
	}
	for _, forbidden := range []string{"password", "admin", "secret-value"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Errorf("pause output leaked %q: %q", forbidden, text)
		}
	}
}

func TestPauseOnFailureStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := pauseOnFailure(ctx, discovery.Plan{Name: "local"}, &casefile.Case{Name: "failed"}, runner.Endpoint{}, runOptions{pauseInput: strings.NewReader("")})
	if err != nil {
		t.Fatalf("pauseOnFailure cancellation: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("pauseOnFailure did not honor cancellation promptly")
	}
}

type pauseErrorReader struct{}

func (pauseErrorReader) Read([]byte) (int, error) { return 0, fmt.Errorf("input failed") }

func TestPauseOnFailureDefaultStreamsAndInputError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pauseOnFailure(ctx, discovery.Plan{Name: "local"}, &casefile.Case{Name: "failed"}, runner.Endpoint{}, runOptions{}); err != nil {
		t.Fatalf("pauseOnFailure defaults: %v", err)
	}
	if err := pauseOnFailure(context.Background(), discovery.Plan{Name: "local"}, &casefile.Case{Name: "failed"}, runner.Endpoint{}, runOptions{pauseInput: pauseErrorReader{}, pauseOutput: io.Discard}); err != nil {
		t.Fatalf("pauseOnFailure input error: %v", err)
	}
}

func TestRunPlanPausesAfterFailure(t *testing.T) {
	plan := discovery.Plan{
		Name:    "remote-pause",
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}},
		Cases:   []*casefile.Case{{Name: "failed"}},
	}
	rep := report.NewTextReporter(io.Discard, report.VerboseDefault)
	res := runPlan(context.Background(), rep, plan, runOptions{
		pauseOnFailure: true,
		pauseInput:     strings.NewReader("\n"),
		pauseOutput:    io.Discard,
		runCase:        func(context.Context, *casefile.Case) bool { return false },
	})
	if res.pass != 0 || res.fail != 1 || res.err != nil {
		t.Fatalf("runPlan pause result = %+v", res)
	}
}

func TestRunPlansPauseStopsAfterFirstFailedPlan(t *testing.T) {
	plans := []discovery.Plan{
		{
			Name:    "first",
			Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}},
			Cases:   []*casefile.Case{{Name: "first failure"}},
		},
		{
			Name:    "second",
			Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}},
			Cases:   []*casefile.Case{{Name: "second failure"}},
		},
	}
	rep := report.NewTextReporter(io.Discard, report.VerboseDefault)
	pass, fail, err := runPlans(context.Background(), rep, plans, runOptions{
		pauseOnFailure: true,
		pauseInput:     strings.NewReader("\n"),
		pauseOutput:    io.Discard,
		runCase:        func(context.Context, *casefile.Case) bool { return false },
	}, 1)
	if err != nil {
		t.Fatalf("runPlans pause: %v", err)
	}
	if pass != 0 || fail != 1 {
		t.Fatalf("runPlans pause result = pass %d, fail %d; want pass 0, fail 1", pass, fail)
	}
}

func TestValidatePauseOnFailure(t *testing.T) {
	compose := discovery.Plan{Name: "compose", Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{}}}
	remote := discovery.Plan{Name: "remote", Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://example"}}}
	tests := []struct {
		name        string
		format      string
		parallel    string
		interactive bool
		plans       []discovery.Plan
		want        string
	}{
		{name: "disabled"},
		{name: "format", format: "ndjson", parallel: "1", interactive: true, plans: []discovery.Plan{compose}, want: "requires --format=text"},
		{name: "parallel", format: "text", parallel: "2", interactive: true, plans: []discovery.Plan{compose}, want: "requires --parallel=1"},
		{name: "noninteractive", format: "text", parallel: "1", interactive: false, plans: []discovery.Plan{compose}, want: "interactive terminal"},
		{name: "fixture", format: "text", parallel: "1", interactive: true, plans: []discovery.Plan{remote}, want: "supports only managed Compose"},
		{name: "valid", format: "TEXT", parallel: "1", interactive: true, plans: []discovery.Plan{compose}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parallel := 1
			if tt.parallel != "" {
				_, _ = fmt.Sscan(tt.parallel, &parallel)
			}
			cli := &usagespec.RunCmd{Format: tt.format, Parallel: parallel, PauseOnFailure: tt.name != "disabled"}
			if cli.Format == "" {
				cli.Format = "text"
			}
			err := validatePauseOnFailure(cli, tt.plans, tt.interactive)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validatePauseOnFailure() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validatePauseOnFailure() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestIsInteractiveStdin(t *testing.T) {
	_ = isInteractiveStdin()
}

func TestIsInteractiveFDRejectsNonTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()
	if isInteractiveFD(int(devNull.Fd())) {
		t.Fatal("non-terminal file descriptor was detected as interactive")
	}
}

func TestWithLGTMVersion(t *testing.T) {
	t.Setenv("LGTM_IMAGE", "")
	original := &casefile.ComposeFixture{
		Env: []string{"FOO=bar"},
	}
	plan := discovery.Plan{Fixture: casefile.FixtureConfig{Compose: original}}

	got := withLGTMVersion(plan, "0.12.2")
	if got.Fixture.Compose == original {
		t.Fatal("withLGTMVersion mutated the discovered fixture")
	}
	want := []string{
		"FOO=bar",
		"LGTM_IMAGE=docker.io/grafana/otel-lgtm:0.12.2",
	}
	if !slices.Equal(got.Fixture.Compose.Env, want) {
		t.Fatalf("compose env = %v, want %v", got.Fixture.Compose.Env, want)
	}
	if len(original.Env) != 1 {
		t.Fatalf("original compose env was mutated: %v", original.Env)
	}

	if unchanged := withLGTMVersion(plan, ""); unchanged.Fixture.Compose != original {
		t.Fatal("empty version should leave the fixture unchanged")
	}

	none := discovery.Plan{Fixture: casefile.FixtureConfig{
		Compose: &casefile.ComposeFixture{Template: "none", File: "compose.yml"},
	}}
	if got := withLGTMVersion(none, "0.12.2"); got.Fixture.Compose != none.Fixture.Compose {
		t.Fatal("version override should not affect template=none fixtures")
	}
}

func TestWithLGTMVersionPreservesFullImageOverride(t *testing.T) {
	t.Run("fixture env", func(t *testing.T) {
		compose := &casefile.ComposeFixture{
			Env: []string{"LGTM_IMAGE=example.invalid/custom:lgtm"},
		}
		plan := discovery.Plan{Fixture: casefile.FixtureConfig{Compose: compose}}
		if got := withLGTMVersion(plan, "0.12.2"); got.Fixture.Compose != compose {
			t.Fatal("fixture LGTM_IMAGE should take precedence over the version")
		}
	})

	t.Run("process env", func(t *testing.T) {
		t.Setenv("LGTM_IMAGE", "example.invalid/custom:lgtm")
		compose := &casefile.ComposeFixture{}
		plan := discovery.Plan{Fixture: casefile.FixtureConfig{Compose: compose}}
		if got := withLGTMVersion(plan, "0.12.2"); got.Fixture.Compose != compose {
			t.Fatal("process LGTM_IMAGE should take precedence over the version")
		}
	})

	t.Run("empty fixture env clears process override", func(t *testing.T) {
		t.Setenv("LGTM_IMAGE", "example.invalid/custom:lgtm")
		compose := &casefile.ComposeFixture{Env: []string{"LGTM_IMAGE="}}
		plan := discovery.Plan{Fixture: casefile.FixtureConfig{Compose: compose}}
		got := withLGTMVersion(plan, "0.12.2")
		if got.Fixture.Compose == compose {
			t.Fatal("empty fixture LGTM_IMAGE should allow the version override")
		}
	})
}

func TestRunActionRejectsMissingConfig(t *testing.T) {
	args := []string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}

	err := execute(args, new(int), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "discovery load") {
		t.Fatalf("Execute error = %v, want discovery load error", err)
	}
}

func TestRunActionRejectsPathFilterWithNoMatches(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	writeFile(t, dir, "oats-config.yaml", `meta:
  version: 3
cases: ["cases/oats-case.yaml"]
`)
	writeFile(t, dir, "cases/oats-case.yaml", `name: smoke
fixture:
  remote:
    endpoint: http://localhost:4318
expected:
  traces:
    - traceql: '{}'
      match_spans:
        - name: smoke
`)

	args := []string{"--config", config, filepath.Join(dir, "not-selected")}
	err := execute(args, new(int), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no cases matched the given path(s)") {
		t.Fatalf("Execute error = %v, want path-filter error", err)
	}
}

func TestRunActionSetsFailureExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gcx is a POSIX shell script")
	}

	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	writeFile(t, dir, "oats-config.yaml", `meta:
  version: 3
cases: ["cases/oats-case.yaml"]
`)
	writeFile(t, dir, "cases/oats-case.yaml", `name: failing
fixture:
  remote:
    endpoint: http://localhost:4318
expected:
  traces:
    - traceql: missing
      match_spans:
        - name: seed-operation
`)

	exit := 0
	args := []string{
		"--config", config,
		"--gcx", fakeGCXPath(t),
		"--timeout", "100ms",
		"--interval", "1ms",
		"--seed-settle", "1ns",
		"--lgtm-version", "0.12.2",
		"--no-cache",
	}
	if err := execute(args, &exit, io.Discard); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if exit != 1 {
		t.Fatalf("exit = %d, want 1 for a failed case", exit)
	}
}

func TestRunMapsCommandErrorsToExitCodeTwo(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"oats", "--config", filepath.Join(t.TempDir(), "missing.yaml")}

	if got := Run(); got != 2 {
		t.Fatalf("Run() = %d, want 2 for a command error", got)
	}
}

func TestSplitCSVAndVerbosityBoundaries(t *testing.T) {
	if got := splitCSV(" traces, ,logs ,, "); len(got) != 2 || got[0] != "traces" || got[1] != "logs" {
		t.Fatalf("splitCSV = %#v", got)
	}
	if got := splitCSV("   "); got != nil {
		t.Fatalf("splitCSV blank = %#v, want nil", got)
	}
	for _, test := range []struct {
		input int
		want  report.Verbosity
	}{
		{input: -1, want: report.VerboseDefault},
		{input: 1, want: report.VerbosePasses},
		{input: 2, want: report.VerboseCmd},
		{input: 3, want: report.VerboseAll},
		{input: 99, want: report.VerboseAll},
	} {
		if got := verbosityFromInt(test.input); got != test.want {
			t.Errorf("verbosityFromInt(%d) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestRunPlansParallelWithEmptyRemoteGroups(t *testing.T) {
	rep := report.NewTextReporter(io.Discard, report.VerboseDefault)
	plans := []discovery.Plan{
		{Name: "one", Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://one"}}},
		{Name: "two", Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://two"}}},
	}
	pass, fail, err := runPlansParallel(context.Background(), rep, plans, runOptions{}, 2)
	if err != nil || pass != 0 || fail != 0 {
		t.Fatalf("runPlansParallel = pass:%d fail:%d err:%v", pass, fail, err)
	}
	if pass, fail, err := runPlansParallel(context.Background(), rep, nil, runOptions{}, 2); err != nil || pass != 0 || fail != 0 {
		t.Fatalf("empty runPlansParallel = pass:%d fail:%d err:%v", pass, fail, err)
	}
}

func TestCLIConfigAndSmallHelpers(t *testing.T) {
	if !contains([]string{"one", "two"}, "two") || contains([]string{"one"}, "missing") {
		t.Fatal("contains returned an unexpected result")
	}

	config := "/explicit/config.yaml"
	if got, err := resolveConfigPath(config); err != nil || got != "/explicit/config.yaml" {
		t.Fatalf("explicit resolveConfigPath = %q, %v", got, err)
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oats-config.yaml"), []byte("meta:\n  version: 3\ncases: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "nested"))
	config = ""
	if got, err := resolveConfigPath(config); err != nil || got != filepath.Join(dir, "oats-config.yaml") {
		t.Fatalf("parent resolveConfigPath = %q, %v", got, err)
	}

	if err := execute([]string{"version"}, new(int), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := execute([]string{"cache"}, new(int), io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRunConfigPathFromPositionalArgument(t *testing.T) {
	cwd := t.TempDir()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(project, "oats-config.yaml")
	if err := os.WriteFile(config, []byte("meta:\n  version: 3\ncases: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	for name, arg := range map[string]string{
		"directory": project,
		"file":      config,
	} {
		t.Run(name, func(t *testing.T) {
			got, paths, err := resolveRunConfigPath("", []string{arg})
			if err != nil {
				t.Fatalf("resolveRunConfigPath: %v", err)
			}
			if got != config {
				t.Fatalf("config = %q, want %q", got, config)
			}
			if paths != nil {
				t.Fatalf("paths = %v, want positional config argument to be consumed", paths)
			}
		})
	}
}

func TestResolveRunConfigPathKeepsFiltersWhenConfigIsDiscovered(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	if err := os.WriteFile(config, []byte("meta:\n  version: 3\ncases: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	args := []string{"cases"}
	got, paths, err := resolveRunConfigPath("", args)
	if err != nil {
		t.Fatalf("resolveRunConfigPath: %v", err)
	}
	if got != config {
		t.Fatalf("config = %q, want %q", got, config)
	}
	if !slices.Equal(paths, args) {
		t.Fatalf("paths = %v, want %v", paths, args)
	}
}

func TestResolveRunConfigPathReportsInvalidPositionalArgument(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	for name, arg := range map[string]string{
		"missing path":             filepath.Join(cwd, "missing"),
		"directory without config": t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := resolveRunConfigPath("", []string{arg})
			if err == nil {
				t.Fatal("resolveRunConfigPath unexpectedly succeeded")
			}
			for _, want := range []string{"no oats-config.yaml found", "cannot use positional config path", arg} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}

	_, paths, err := resolveRunConfigPath("", []string{"one", "two"})
	if err == nil || !strings.Contains(err.Error(), "no oats-config.yaml found") {
		t.Fatalf("multi-path error = %v, want config discovery error", err)
	}
	if !slices.Equal(paths, []string{"one", "two"}) {
		t.Fatalf("paths = %v, want original positional paths", paths)
	}
}

func TestCLIListMigrateAndCacheCommands(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	writeFile(t, dir, "oats-config.yaml", `meta:
  version: 3
cases: ["case.yaml"]
`)
	writeFile(t, dir, "case.yaml", `name: smoke
fixture:
  remote:
    endpoint: http://localhost:4318
expected:
  traces:
    - traceql: '{}'
      match_spans:
        - name: smoke
`)

	listArgs := []string{"list", "--config", config}
	if err := execute(listArgs, new(int), io.Discard); err != nil {
		t.Fatalf("list command: %v", err)
	}

	legacy := filepath.Join(dir, "legacy.oats.yaml")
	writeFile(t, dir, filepath.Base(legacy), `oats-schema-version: 2
expected:
  custom-checks:
    - script: true
`)
	migrateArgs := []string{"migrate", legacy}
	if err := execute(migrateArgs, new(int), io.Discard); err != nil {
		t.Fatalf("migrate file command: %v", err)
	}

	migrateDir := filepath.Join(dir, "legacy-tree")
	if err := os.MkdirAll(migrateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, migrateDir, "legacy.oats.yaml", `oats-schema-version: 2
expected:
  custom-checks:
    - script: true
`)
	migrateArgs = []string{"migrate", migrateDir}
	if err := execute(migrateArgs, new(int), io.Discard); err != nil {
		t.Fatalf("migrate directory command: %v", err)
	}

	cacheDir := filepath.Join(dir, "cache")
	store, err := cache.New(cacheDir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(cache.Key{CaseYAML: []byte("case")}); err != nil {
		t.Fatal(err)
	}
	cacheArgs := []string{"cache", "clear", "--cache-dir", cacheDir}
	if err := execute(cacheArgs, new(int), io.Discard); err != nil {
		t.Fatalf("cache clear command: %v", err)
	}
}

func TestDeprecatedListAndMigrateFlags(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	writeFile(t, dir, "oats-config.yaml", `meta:
  version: 3
cases: ["case.yaml"]
`)
	writeFile(t, dir, "case.yaml", `name: smoke
fixture:
  remote:
    endpoint: http://localhost:4318
expected:
  traces:
    - traceql: '{}'
`)

	args := []string{"--config", config, "--list"}
	if err := execute(args, new(int), io.Discard); err != nil {
		t.Fatalf("deprecated --list: %v", err)
	}

	legacy := filepath.Join(dir, "legacy.oats.yaml")
	writeFile(t, dir, filepath.Base(legacy), `oats-schema-version: 2
expected:
  custom-checks:
    - script: true
`)
	args = []string{"--migrate", legacy}
	if err := execute(args, new(int), io.Discard); err != nil {
		t.Fatalf("deprecated --migrate: %v", err)
	}
}

func TestRunActionRejectsFilterAndRuntimeErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oats-config.yaml")
	writeFile(t, dir, "oats-config.yaml", `meta:
  version: 3
cases: ["case.yaml"]
`)
	writeFile(t, dir, "case.yaml", `name: smoke
tags: [smoke]
fixture:
  remote:
    endpoint: http://localhost:4318
expected:
  traces:
    - traceql: '{}'
`)

	args := []string{"--config", config, "--tags", "missing"}
	if err := execute(args, new(int), io.Discard); err == nil || !strings.Contains(err.Error(), "no cases matched the filter") {
		t.Fatalf("filter error = %v", err)
	}

	args = []string{"--config", config, "--pause-on-failure", "--format", "ndjson"}
	if err := execute(args, new(int), io.Discard); err == nil || !strings.Contains(err.Error(), "requires --format=text") {
		t.Fatalf("pause validation error = %v", err)
	}

	args = []string{"--config", config, "--container-runtime", "invalid"}
	if err := execute(args, new(int), io.Discard); err == nil || !strings.Contains(err.Error(), "unsupported container runtime") {
		t.Fatalf("runtime error = %v", err)
	}
}

func TestCLIReporterAndRunPlanCache(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "report-*.log")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"ndjson", "JSON", "text"} {
		rep := newReporter(file, format, report.VerboseDefault)
		rep.Emit(report.Event{Type: report.EventRunStart})
		if err := rep.Close(); err != nil {
			t.Fatalf("close %s reporter: %v", format, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	plan := discovery.Plan{
		Name:    "remote-cache",
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}},
	}
	rep := report.NewTextReporter(io.Discard, report.VerboseDefault)
	res := runPlan(context.Background(), rep, plan, runOptions{cacheDir: t.TempDir()})
	if res.err != nil {
		t.Fatalf("runPlan with cache: %+v", res)
	}
}

func TestRunPlansNormalizesParallelism(t *testing.T) {
	plans := []discovery.Plan{
		{Name: "one", Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://one"}}},
		{Name: "two", Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://two"}}},
	}
	rep := report.NewTextReporter(io.Discard, report.VerboseDefault)
	if pass, fail, err := runPlans(context.Background(), rep, plans, runOptions{}, 0); err != nil || pass != 0 || fail != 0 {
		t.Fatalf("sequential runPlans = pass:%d fail:%d err:%v", pass, fail, err)
	}
	if pass, fail, err := runPlans(context.Background(), rep, plans, runOptions{}, 2); err != nil || pass != 0 || fail != 0 {
		t.Fatalf("parallel runPlans = pass:%d fail:%d err:%v", pass, fail, err)
	}
}

func TestCLIPathVersionAndReporterHelpers(t *testing.T) {
	if got, err := absArgs([]string{"cases", "./case.yaml"}); err != nil || len(got) != 2 || !filepath.IsAbs(got[0]) || !filepath.IsAbs(got[1]) {
		t.Fatalf("absArgs = %v, %v", got, err)
	}
	if got, err := absArgs(nil); err != nil || got != nil {
		t.Fatalf("empty absArgs = %v, %v", got, err)
	}

	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := resolveConfigPath(""); err == nil {
		t.Fatal("resolveConfigPath should fail when no default config exists")
	}

	script := filepath.Join(dir, "gcx-version")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'gcx 0.4.3\\nbuild details\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := gcxVersion(script); got != "gcx 0.4.3" {
		t.Fatalf("gcxVersion = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	if got := defaultCacheDir(); got != filepath.Join(dir, "state", "oats") {
		t.Fatalf("defaultCacheDir = %q", got)
	}

	inner := &recordingReporter{}
	locked := &lockedReporter{inner: inner}
	locked.Emit(report.Event{Type: report.EventRunStart})
	if err := locked.Close(); err != nil || len(inner.events) != 1 {
		t.Fatalf("lockedReporter = events:%v err:%v", inner.events, err)
	}
}
