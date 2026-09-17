package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/internal/cli/usagespec"
)

func TestUsageRunContract(t *testing.T) {
	for _, prefix := range [][]string{nil, {"run"}} {
		args := append(append([]string{}, prefix...), "cases/one", "--timeout", "2s", "-vvv", "--no-cache=false", "--parallel=2", "--", "--literal-path")
		line, err := parseCommand(args)
		if err != nil {
			t.Fatal(err)
		}
		if line.Verbose != 3 || line.run.timeout != 2*time.Second || line.run.NoCache || line.run.parallel != 2 {
			t.Fatalf("incorrect run options: %+v; verbosity %d", line.run, line.Verbose)
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
	if line.run.timeout != -time.Second || line.Verbose != 3 {
		t.Fatalf("timeout=%s verbosity=%d", line.run.timeout, line.Verbose)
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
	if line.Verbose != 1 || line.run.NoCache || line.run.timeout != 30*time.Second {
		t.Fatal("CLI must override environment")
	}
	if line.run.Gcx != "gcx" || line.run.Config != "" || line.run.LgtmVersion != "" {
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
	if line.run.timeout != 2*time.Second || line.run.Gcx != "/opt/tools/gcx" ||
		line.run.GcxDownload != "never" || !line.run.NoCache || line.run.GcxVersion != "0.4.3" {
		t.Fatalf("environment resolution: %+v", line.run)
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
