package compose

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/oats/testhelpers/container"
	"github.com/grafana/oats/testhelpers/remote"
)

func TestStackFilesWithRuntimeLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}

	dir := t.TempDir()
	baseFile := filepath.Join(dir, "base.yml")
	overrideFile := filepath.Join(dir, "override.yml")
	for path, contents := range map[string]string{
		baseFile:     "services:\n  app:\n    profiles: [app-profile]\n",
		overrideFile: "services:\n  app:\n    profiles: ['${EXTRA_PROFILE}']\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	argsFile := filepath.Join(dir, "args")
	command := filepath.Join(dir, "compose")
	if err := os.WriteFile(command, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$COMPOSE_TEST_ARGS"
for arg in "$@"; do
  printf 'arg=<%s>\n' "$arg" >> "$COMPOSE_TEST_ARGS"
done
case "$*" in
*logs*)
  printf 'service ready\n'
  ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSE_TEST_ARGS", argsFile)

	c, err := StackFilesWithRuntime([]string{baseFile, overrideFile}, []string{"COMPOSE_PROJECT_NAME=oats-test", "EXTRA_PROFILE=from-override"}, container.Podman)
	if err != nil {
		t.Fatalf("StackFilesWithRuntime: %v", err)
	}
	c.Command = command

	if err := c.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := c.Run(context.Background(), "app", []string{"mise", "run", "hello world"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var output strings.Builder
	if err := c.LogsToConsumer(func(r io.ReadCloser, wg *sync.WaitGroup) {
		defer wg.Done()
		body, err := io.ReadAll(r)
		if err != nil {
			t.Errorf("read logs: %v", err)
			return
		}
		output.Write(body)
	}); err != nil {
		t.Fatalf("LogsToConsumer: %v", err)
	}
	if output.String() != "service ready\n" {
		t.Fatalf("logs = %q", output.String())
	}
	if err := c.LogToStdout(); err != nil {
		t.Fatalf("LogToStdout: %v", err)
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := c.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"network prune -f --filter until=5m",
		"-f " + baseFile + " -f " + overrideFile + " up --build --detach --force-recreate",
		"-f " + baseFile + " -f " + overrideFile + " --profile app-profile --profile from-override build app",
		"-f " + baseFile + " -f " + overrideFile + " --profile app-profile --profile from-override run --rm --no-deps app mise run hello world",
		"arg=<hello world>",
		"-f " + baseFile + " -f " + overrideFile + " logs",
		"-f " + baseFile + " -f " + overrideFile + " stop",
		"-f " + baseFile + " -f " + overrideFile + " down",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("command log %q missing %q", data, want)
		}
	}
}

func TestRunReportsBuildAndCommandFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}

	for _, tc := range []struct {
		name string
		fail string
		want string
	}{
		{name: "build", fail: "build", want: `failed to build compose service "app"`},
		{name: "run", fail: "run", want: `failed to run compose service "app"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := filepath.Join(t.TempDir(), "compose")
			script := "#!/bin/sh\ncase \"$*\" in\n  *" + tc.fail + "*) exit 1 ;;\nesac\n"
			if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			c, err := StackFilesWithRuntime([]string{"compose.yml"}, nil, container.Docker)
			if err != nil {
				t.Fatal(err)
			}
			c.Command = command
			if err := c.Run(context.Background(), "app", []string{"true"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunRetainsDockerWildcardProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	command := filepath.Join(dir, "compose")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$COMPOSE_TEST_ARGS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSE_TEST_ARGS", argsFile)
	c, err := StackFilesWithRuntime([]string{"compose.yml"}, nil, container.Docker)
	if err != nil {
		t.Fatal(err)
	}
	c.Command = command
	if err := c.Run(context.Background(), "app", []string{"true"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "compose -f compose.yml --profile * build app\ncompose -f compose.yml --profile * run --rm --no-deps app true\n"; got != want {
		t.Fatalf("Docker compose calls = %q, want %q", got, want)
	}
}

func TestComposeValidationAndEnvironmentMerge(t *testing.T) {
	if _, err := StackFilesWithRuntime(nil, nil, container.Docker); err == nil {
		t.Fatal("expected missing compose file error")
	}
	if _, err := StackFilesWithRuntime([]string{"compose.yml"}, nil, container.Engine("unknown")); err == nil {
		t.Fatal("expected unsupported runtime error")
	}

	got := mergeEnv([]string{"A=parent", "B=parent", "NO_EQUALS"}, []string{"A=override", "C=child"})
	want := []string{"A=override", "B=parent", "NO_EQUALS", "C=child"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("mergeEnv = %#v, want %#v", got, want)
	}

	c, err := SuiteFilesWithRuntime([]string{"compose.yml"}, nil, container.Docker)
	if err != nil {
		t.Fatalf("SuiteFilesWithRuntime: %v", err)
	}
	if c.Command != "docker" || len(c.DefaultArgs) == 0 {
		t.Fatalf("unexpected Docker suite: %+v", c)
	}
	if suite, err := Suite("compose.yml"); err != nil || suite == nil {
		t.Fatalf("Suite: %v", err)
	}
	if stack, err := Stack("compose.yml"); err != nil || stack == nil {
		t.Fatalf("Stack: %v", err)
	}
	if stack, err := StackFiles([]string{"compose.yml"}, nil); err != nil || stack == nil {
		t.Fatalf("StackFiles: %v", err)
	}
	if endpoint := NewEndpoint("localhost", "compose.yml", remote.PortsConfig{}); endpoint == nil {
		t.Fatal("NewEndpoint returned nil")
	}
	if endpoint := NewEndpointWithRuntime("localhost", "compose.yml", remote.PortsConfig{}, container.Podman); endpoint == nil {
		t.Fatal("NewEndpointWithRuntime returned nil")
	}
}

func TestComposeCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}

	command := filepath.Join(t.TempDir(), "compose-fail")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := StackFilesWithRuntime([]string{"compose.yml"}, nil, container.Docker)
	if err != nil {
		t.Fatal(err)
	}
	c.Command = command
	if err := c.Stop(); err == nil || !strings.Contains(err.Error(), "failed to run compose command") {
		t.Fatalf("Stop error = %v", err)
	}
}

func TestServiceProfiles(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	override := filepath.Join(dir, "override.yml")
	if err := os.WriteFile(base, []byte("services:\n  app:\n    profiles: [first, shared]\n  plain:\n    image: scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("services:\n  app:\n    profiles: [second, shared]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := serviceProfiles(context.Background(), []string{base, override}, nil, "app")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "shared", "second"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("serviceProfiles = %v, want %v", got, want)
	}

	// A service with no profile remains available without profile flags.
	got, err = serviceProfiles(context.Background(), []string{base}, nil, "plain")
	if err != nil || len(got) != 0 {
		t.Fatalf("unprofiled service profiles = %v, %v; want none", got, err)
	}

	for _, tc := range []struct {
		name      string
		contents  string
		wantError string
	}{
		{name: "malformed", contents: "services: [", wantError: "parse"},
		{name: "unsupported interpolation", contents: "services:\n  app:\n    profiles: ['${MISSING:-fallback}']\n", wantError: "unsupported or unresolved interpolation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "compose.yml")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := serviceProfiles(context.Background(), []string{path}, nil, "app"); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("serviceProfiles error = %v, want %q", err, tc.wantError)
			}
		})
	}
	if _, err := serviceProfiles(context.Background(), []string{filepath.Join(dir, "missing.yml")}, nil, "app"); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("missing file error = %v, want read error", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := serviceProfiles(canceled, []string{base}, nil, "app"); err != context.Canceled {
		t.Fatalf("canceled lookup error = %v, want context.Canceled", err)
	}
}
