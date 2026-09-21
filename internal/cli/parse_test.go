package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/internal/cli/usagespec"
	"github.com/jdx/usage/go/argv"
)

func TestUsageRunContract(t *testing.T) {
	for _, prefix := range [][]string{nil, {"run"}} {
		args := append(append([]string{}, prefix...), "cases/one", "--timeout", "2s", "-vvv", "--no-cache=false", "--parallel=2", "--", "--literal-path")
		line, err := parseCommand(args)
		if err != nil {
			t.Fatal(err)
		}
		if line.Verbose != 3 || line.Run.Timeout != 2*time.Second || line.Run.NoCache || line.Run.Parallel != 2 {
			t.Fatalf("incorrect run options: %+v; verbosity %d", line.Run, line.Verbose)
		}
		if strings.Join(line.Run.Paths, ",") != "cases/one,--literal-path" {
			t.Fatalf("paths = %v", line.Run.Paths)
		}
	}
}

func TestUsageInheritedVerbosityAndAttachedNegativeDuration(t *testing.T) {
	line, err := parseCommand([]string{"-v", "run", "--timeout=-1s", "-vv"})
	if err != nil {
		t.Fatal(err)
	}
	if line.Run.Timeout != -time.Second || line.Verbose != 3 {
		t.Fatalf("timeout=%s verbosity=%d", line.Run.Timeout, line.Verbose)
	}
}

func TestUsageEnvironmentAndExplicitDefaults(t *testing.T) {
	t.Setenv("OATS_VERBOSE", "2")
	t.Setenv("OATS_TIMEOUT", "invalid")
	t.Setenv("OATS_NO_CACHE", "true")
	t.Setenv("OATS_GCX", "gcx")
	line, err := parseCommand([]string{"run", "--timeout=30s", "--no-cache=false", "-v"})
	if err != nil {
		t.Fatal(err)
	}
	if line.Verbose != 1 || line.Run.NoCache || line.Run.Timeout != 30*time.Second {
		t.Fatal("CLI must override environment")
	}
	if line.Run.Gcx != "gcx" || line.Run.Config != "" || line.Run.LgtmVersion != "" {
		t.Fatal("explicit gcx must be retained; config/LGTM defaults must remain unset")
	}
	if _, err := parseCommand(nil); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("invalid duration env: %v", err)
	}
}

func TestUsageEnvironmentResolution(t *testing.T) {
	t.Setenv("OATS_TIMEOUT", "2s")
	t.Setenv("OATS_GCX", "/opt/tools/gcx")
	t.Setenv("OATS_GCX_DOWNLOAD", "never")
	t.Setenv("OATS_NO_CACHE", "true")
	line, err := parseCommand([]string{"--gcx-version", "0.4.3"})
	if err != nil {
		t.Fatal(err)
	}
	if line.Run.Timeout != 2*time.Second || line.Run.Gcx != "/opt/tools/gcx" ||
		line.Run.GcxDownload != "never" || !line.Run.NoCache || line.Run.GcxVersion != "0.4.3" {
		t.Fatalf("environment resolution: %+v", line.Run)
	}
}

func TestUsageOptionalOverrides(t *testing.T) {
	for _, env := range []string{"OATS_CONFIG", "OATS_GCX", "OATS_LGTM_VERSION"} {
		t.Setenv(env, "")
	}
	implicit, err := parseCommand(nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := parseCommand([]string{"--config=oats-config.yaml", "--gcx=gcx", "--lgtm-version=latest"})
	if err != nil {
		t.Fatal(err)
	}
	if implicit.Run.Config != "" || implicit.Run.Gcx != "" || implicit.Run.LgtmVersion != "" {
		t.Fatal("optional empty overrides must be unset")
	}
	if explicit.Run.Config != "oats-config.yaml" || explicit.Run.Gcx != "gcx" || explicit.Run.LgtmVersion != "latest" {
		t.Fatal("explicit values equal to application defaults must remain overrides")
	}
	t.Setenv("OATS_CONFIG", "/env/config")
	t.Setenv("OATS_GCX", "/env/gcx")
	t.Setenv("OATS_LGTM_VERSION", "0.1.0")
	cleared, err := parseCommand([]string{"--config=", "--gcx=", "--lgtm-version="})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cleared.Run, implicit.Run) {
		t.Fatal("empty CLI values should clear environment overrides")
	}
}

func TestUsageVerbosityEnvironment(t *testing.T) {
	for _, value := range []string{"0", "3"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("OATS_VERBOSE", value)
			line, err := parseCommand(nil)
			if err != nil {
				t.Fatal(err)
			}
			if line.Verbose != int(value[0]-'0') {
				t.Fatalf("verbosity=%d", line.Verbose)
			}
		})
	}
	for _, value := range []string{"", "-1", "invalid", "999999999999999999999999"} {
		t.Run("invalid:"+value, func(t *testing.T) {
			t.Setenv("OATS_VERBOSE", value)
			if _, err := parseCommand(nil); err == nil || !strings.Contains(err.Error(), "OATS_VERBOSE") {
				t.Fatalf("invalid count environment: %v", err)
			}
			line, err := parseCommand([]string{"-v"})
			if err != nil || line.Verbose != 1 {
				t.Fatalf("CLI should win: %+v, %v", line, err)
			}
		})
	}
}

func TestUsageRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--unknown"}, {"--timeout"}, {"--timeout", "invalid"},
		{"--parallel", "invalid"}, {"--no-cache=maybe"},
		{"migrate"}, {"migrate", "one", "two"}, {"cache", "unknown"},
		{"completion"}, {"completion", "unknown"}, {"list", "--timeout", "1s"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseCommand(args); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestUsageOutputDoesNotRunCases(t *testing.T) {
	t.Setenv("OATS_CONFIG", "/missing/config.yaml")
	t.Setenv("OATS_TIMEOUT", "invalid")
	t.Setenv("OATS_VERBOSE", "invalid")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "OpenTelemetry Acceptance Tests"},
		{[]string{"-h"}, "OpenTelemetry Acceptance Tests"},
		{[]string{"run", "--help"}, "Run cases"},
		{[]string{"help", "migrate"}, "Usage: oats migrate"},
		{[]string{"help", "cache", "clear"}, "Usage: oats cache clear"},
		{[]string{"cache"}, "clear"},
		{[]string{"version"}, Version},
		{[]string{"--version"}, Version},
		{[]string{"usage"}, usagespec.Spec},
		{[]string{"completion", "bash"}, "__complete"},
		{[]string{"__complete_word__", "--shell", "bash", "--line", "oats --gcx-v"}, "--gcx-version"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			exit := 0
			if err := execute(tc.args, &exit, &out); err != nil {
				t.Fatal(err)
			}
			if exit != 0 || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("exit=%d output=%q, want %q", exit, out.String(), tc.want)
			}
		})
	}
}

// TestUsageTypedAndHelpRegressions protects Usage-native help routing and typed
// value validation at OATS' application boundary.
func TestUsageTypedAndHelpRegressions(t *testing.T) {
	t.Run("separator keeps help positional", func(t *testing.T) {
		line, err := parseCommand([]string{"run", "--", "--help"})
		if err != nil {
			t.Fatal(err)
		}
		if len(line.Run.Paths) != 1 || line.Run.Paths[0] != "--help" {
			t.Fatalf("paths=%v", line.Run.Paths)
		}
	})
	t.Run("unknown before help is an error", func(t *testing.T) {
		var out bytes.Buffer
		if err := execute([]string{"--unknown", "--help"}, new(int), &out); err == nil || strings.Contains(out.String(), "Usage:") {
			t.Fatalf("err=%v output=%q", err, out.String())
		}
	})
	t.Run("help bypasses invalid environment", func(t *testing.T) {
		t.Setenv("OATS_TIMEOUT", "invalid")
		t.Setenv("OATS_PARALLEL", "invalid")
		t.Setenv("OATS_VERBOSE", "invalid")
		for _, args := range [][]string{{"--help"}, {"-h"}, {"run", "--help"}, {"list", "--help"}, {"migrate", "--help"}, {"cache", "clear", "--help"}} {
			var out bytes.Buffer
			if err := execute(args, new(int), &out); err != nil || !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("args=%v err=%v output=%q", args, err, out.String())
			}
		}
	})
	t.Run("typed overflow and override", func(t *testing.T) {
		for _, args := range [][]string{{"run", "--parallel", "999999999999999999999999"}, {"run", "--app-port", "999999999999999999999999"}, {"run", "--timeout", "999999999999999999999999999999999999999999s"}} {
			if _, err := parseCommand(args); err == nil {
				t.Fatalf("args=%v accepted overflow", args)
			} else if parseErr, ok := err.(*argv.Error); !ok || parseErr.Code != argv.CodeInvalidValue {
				t.Fatalf("args=%v error=%T %v", args, err, err)
			}
		}
		t.Setenv("OATS_PARALLEL", "invalid")
		if line, err := parseCommand([]string{"run", "--parallel", "2"}); err != nil || line.Run.Parallel != 2 {
			t.Fatalf("override line=%v err=%v", line, err)
		}
	})
}

// Verify every run flag's CLI and environment wiring against application values,
// including options that are normally only exercised by fixture execution.
func TestUsageAllRunOptionSources(t *testing.T) {
	flags := []struct{ name, value string }{
		{"config", "custom/config.yaml"},
		{"gcx", "/opt/gcx"},
		{"gcx-version", "0.4.4"},
		{"gcx-download", "never"},
		{"format", "ndjson"},
		{"tags", "smoke,fast"},
		{"timeout", "1m"},
		{"interval", "250ms"},
		{"absent-timeout", "5s"},
		{"seed-settle", "3s"},
		{"gcx-context", "staging"},
		{"lgtm-version", "0.12.2"},
		{"container-runtime", "podman"},
		{"app-host", "app.example"},
		{"app-port", "8081"},
		{"otlp-http", "http://collector:4318"},
		{"parallel", "3"},
		{"fail-fast", "true"},
		{"no-cache", "true"},
		{"cache-dir", "custom/cache"},
		{"list", "true"},
		{"migrate", "legacy.yaml"},
	}
	want := &usagespec.RunCmd{
		Config: "custom/config.yaml", Gcx: "/opt/gcx", GcxVersion: "0.4.4",
		GcxDownload: "never", Format: "ndjson", Tags: "smoke,fast",
		Timeout: time.Minute, Interval: 250 * time.Millisecond, AbsentTimeout: 5 * time.Second, SeedSettle: 3 * time.Second,
		GcxContext: "staging", LgtmVersion: "0.12.2", ContainerRuntime: "podman",
		AppHost: "app.example", AppPort: 8081, OtlpHttp: "http://collector:4318",
		Parallel: 3, FailFast: true, NoCache: true, CacheDir: "custom/cache",
		List: true, Migrate: "legacy.yaml",
	}
	for _, source := range []string{"environment", "CLI overrides environment"} {
		t.Run(source, func(t *testing.T) {
			var args []string
			for _, flag := range flags {
				value := flag.value
				if source == "CLI overrides environment" {
					value = "overridden"
					args = append(args, "--"+flag.name+"="+flag.value)
				}
				t.Setenv("OATS_"+strings.ToUpper(strings.ReplaceAll(flag.name, "-", "_")), value)
			}
			line, err := parseCommand(args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(line.Run, want) {
				t.Fatalf("run options = %+v, want %+v", line.Run, want)
			}
			if line.Run.Timeout != time.Minute || line.Run.Interval != 250*time.Millisecond ||
				line.Run.AbsentTimeout != 5*time.Second || line.Run.SeedSettle != 3*time.Second ||
				line.Run.AppPort != 8081 || line.Run.Parallel != 3 {
				t.Fatalf("typed options = %+v", line.Run)
			}
		})
	}
}

func TestUsageBooleanLastOccurrenceWins(t *testing.T) {
	for _, name := range []string{"fail-fast", "no-cache", "list"} {
		t.Run(name, func(t *testing.T) {
			for _, value := range []bool{false, true} {
				args := []string{"--" + name, "--" + name + "=false"}
				if value {
					args = append(args, "--"+name)
				}
				line, err := parseCommand(args)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]bool{
					"fail-fast": line.Run.FailFast, "no-cache": line.Run.NoCache, "list": line.Run.List,
				}[name]
				if got != value {
					t.Fatalf("%v: got %v, want %v", args, got, value)
				}
			}
		})
	}
}

func TestUsageSubcommandEnvironment(t *testing.T) {
	t.Setenv("OATS_CONFIG", "env/config.yaml")
	t.Setenv("OATS_CACHE_DIR", "env/cache")
	line, err := parseCommand([]string{"list"})
	if err != nil || line.List.Config != "env/config.yaml" {
		t.Fatalf("list environment: %+v, %v", line, err)
	}
	line, err = parseCommand([]string{"cache", "clear"})
	if err != nil || line.Cache.Clear.CacheDir != "env/cache" {
		t.Fatalf("cache environment: %+v, %v", line, err)
	}
	line, err = parseCommand([]string{"cache", "clear", "--cache-dir", "cli/cache"})
	if err != nil || line.Cache.Clear.CacheDir != "cli/cache" {
		t.Fatalf("cache CLI precedence: %+v, %v", line, err)
	}
}

func TestUsageExecutionErrors(t *testing.T) {
	var out bytes.Buffer
	if err := execute([]string{"--timeout=invalid"}, new(int), &out); err == nil {
		t.Fatal("invalid duration should propagate through execute")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := execute([]string{"cache", "clear", "--cache-dir", path}, new(int), &out); err == nil {
		t.Fatal("cache directory creation error should propagate through execute")
	}
}
