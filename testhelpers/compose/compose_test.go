package compose

import (
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
	argsFile := filepath.Join(dir, "args")
	command := filepath.Join(dir, "compose")
	if err := os.WriteFile(command, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$COMPOSE_TEST_ARGS"
case "$*" in
*logs*)
  printf 'service ready\n'
  ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSE_TEST_ARGS", argsFile)

	c, err := StackFilesWithRuntime([]string{"base.yml", "override.yml"}, []string{"COMPOSE_PROJECT_NAME=oats-test"}, container.Podman)
	if err != nil {
		t.Fatalf("StackFilesWithRuntime: %v", err)
	}
	c.Command = command

	if err := c.Up(); err != nil {
		t.Fatalf("Up: %v", err)
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
		"-f base.yml -f override.yml up --build --detach --force-recreate",
		"-f base.yml -f override.yml logs",
		"-f base.yml -f override.yml stop",
		"-f base.yml -f override.yml rm -f",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("command log %q missing %q", data, want)
		}
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
