package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/report"
	"github.com/spf13/pflag"
)

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

	if err := newVersionCmd().RunE(newVersionCmd(), nil); err != nil {
		t.Fatalf("version command: %v", err)
	}
	if cmd := newCacheCmd(); cmd == nil || len(cmd.Commands()) != 1 {
		t.Fatal("cache command was not constructed with clear subcommand")
	}
}
