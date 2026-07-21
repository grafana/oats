package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/report"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestRootAndRunCommandsRegisterTheSameRunFlags(t *testing.T) {
	root := newRootCmd(new(int))
	var run *cobra.Command
	for _, command := range root.Commands() {
		if command.Name() == "run" {
			run = command
			break
		}
	}
	if run == nil {
		t.Fatal("run command was not registered")
	}

	for _, name := range []string{"config", "gcx", "gcx-version", "gcx-download", "timeout", "parallel", "no-cache"} {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("root command missing --%s", name)
		}
		if run.Flags().Lookup(name) == nil {
			t.Errorf("run command missing --%s", name)
		}
	}
	if !root.SilenceUsage || !root.SilenceErrors || !run.SilenceUsage || !run.SilenceErrors {
		t.Fatal("runtime commands should suppress Cobra usage and duplicate errors")
	}
}

func TestRunActionRejectsMissingConfig(t *testing.T) {
	root := newRootCmd(new(int))
	root.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := root.Execute()
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

	root := newRootCmd(new(int))
	root.SetArgs([]string{"--config", config, filepath.Join(dir, "not-selected")})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no cases matched the given path(s)") {
		t.Fatalf("Execute error = %v, want path-filter error", err)
	}
}

func TestRunActionRejectsConflictingGCXFlags(t *testing.T) {
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

	root := newRootCmd(new(int))
	root.SetArgs([]string{
		"--config", config,
		"--gcx", "gcx",
		"--gcx-version", "0.4.3",
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--gcx and --gcx-version cannot be used together") {
		t.Fatalf("Execute error = %v, want gcx conflict", err)
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
	root := newRootCmd(&exit)
	root.SetArgs([]string{
		"--config", config,
		"--gcx", fakeGCXPath(t),
		"--timeout", "100ms",
		"--interval", "1ms",
		"--seed-settle", "1ns",
		"--no-cache",
	})
	if err := root.Execute(); err != nil {
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

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("config", "oats-config.yaml", "config")
	if err := fs.Set("config", "/explicit/config.yaml"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveConfigPath(fs); err != nil || got != "/explicit/config.yaml" {
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
	fs = pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("config", "oats-config.yaml", "config")
	if got, err := resolveConfigPath(fs); err != nil || got != filepath.Join(dir, "oats-config.yaml") {
		t.Fatalf("parent resolveConfigPath = %q, %v", got, err)
	}

	versionCmd := newVersionCmd()
	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("version command: %v", err)
	}
	if cmd := newCacheCmd(); cmd == nil || len(cmd.Commands()) != 1 {
		t.Fatal("cache command was not constructed with clear subcommand")
	}
}
