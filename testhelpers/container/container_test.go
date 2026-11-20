package container

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Engine
	}{
		{name: "empty", want: Auto},
		{name: "auto", value: "AUTO", want: Auto},
		{name: "docker", value: " docker ", want: Docker},
		{name: "podman", value: "Podman", want: Podman},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.value)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}

	if _, err := Parse("containerd"); err == nil {
		t.Fatal("Parse(containerd) unexpectedly succeeded")
	}
}

func TestComposeArgs(t *testing.T) {
	for _, engine := range []Engine{Docker, Podman} {
		got := engine.ComposeArgs("-f", "compose.yml", "up")
		want := []string{"compose", "-f", "compose.yml", "up"}
		if len(got) != len(want) {
			t.Fatalf("%s args = %v, want %v", engine, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s args = %v, want %v", engine, got, want)
			}
		}
	}
}

func TestResolveExplicitDoesNotFallback(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}
	path := filepath.Join(dir, "podman")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := Resolve("podman")
	if err != nil {
		t.Fatalf("Resolve(podman): %v", err)
	}
	if got != Podman {
		t.Fatalf("Resolve(podman) = %q, want %q", got, Podman)
	}
	if _, err := Resolve("docker"); err == nil {
		t.Fatal("Resolve(docker) unexpectedly fell back to podman")
	}
}

func TestResolveDockerRequiresCompose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}

	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if _, err := Resolve("docker"); err == nil {
		t.Fatal("Resolve(docker) unexpectedly succeeded without Docker Compose")
	}
}

func TestResolveAutoPrefersPodman(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX executables")
	}
	for _, name := range []string{"podman", "docker"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	got, err := Resolve("auto")
	if err != nil {
		t.Fatalf("Resolve(auto): %v", err)
	}
	if got != Podman {
		t.Fatalf("Resolve(auto) = %q, want %q", got, Podman)
	}
}

func TestResolveAutoFallsBackWhenPodmanEngineIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX executables")
	}
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte(`#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := Resolve("auto")
	if err != nil {
		t.Fatalf("Resolve(auto): %v", err)
	}
	if got != Docker {
		t.Fatalf("Resolve(auto) = %q, want %q", got, Docker)
	}
}

func TestResolveAutoReportsBothEngineFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX executables")
	}
	dir := t.TempDir()
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte(`#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  echo podman-provider-failed >&2
  exit 1
fi
exit 0
`), 0o700); err != nil {
		t.Fatal(err)
	}
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(`#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
echo docker-engine-failed >&2
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := Resolve("auto")
	if err == nil {
		t.Fatal("Resolve(auto) unexpectedly succeeded")
	}
	message := err.Error()
	if !strings.Contains(message, "no supported container runtime") {
		t.Errorf("Resolve(auto) error %q does not retain generic prefix", message)
	}
	for _, reason := range []string{"podman: compose unavailable", "podman-provider-failed", "docker: container engine unavailable", "docker-engine-failed"} {
		if !strings.Contains(message, reason) {
			t.Errorf("Resolve(auto) error %q does not contain %q", message, reason)
		}
	}
	if strings.Index(message, "podman: ") > strings.Index(message, "docker: ") {
		t.Errorf("Resolve(auto) failures are not in preference order: %q", message)
	}
}

func TestResolveAutoPreservesProbeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX executables")
	}
	dir := t.TempDir()
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skip("test requires /bin/sleep")
	}
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte(`#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exec /bin/sleep 6
fi
exit 0
`), 0o700); err != nil {
		t.Fatal(err)
	}
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(`#!/bin/sh
echo docker-provider-failed >&2
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := Resolve("auto")
	if err == nil {
		t.Fatal("Resolve(auto) unexpectedly succeeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Resolve(auto) error %v does not retain context deadline", err)
	}
	message := err.Error()
	for _, reason := range []string{"podman: compose unavailable", "compose probe timed out after", "docker: compose unavailable", "docker-provider-failed"} {
		if !strings.Contains(message, reason) {
			t.Errorf("Resolve(auto) error %q does not contain %q", message, reason)
		}
	}
}

func TestResolveAutoUsesPortableComposeProbe(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX executables")
	}
	podman := filepath.Join(dir, "podman")
	if err := os.WriteFile(podman, []byte(`#!/bin/sh
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
if [ "$1" = "compose" ] && [ "$2" = "-f" ] && [ "$4" = "ps" ] && [ "$#" -eq 4 ]; then
  exit 0
fi
exit 1
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := Resolve("auto")
	if err != nil {
		t.Fatalf("Resolve(auto): %v", err)
	}
	if got != Podman {
		t.Fatalf("Resolve(auto) = %q, want %q", got, Podman)
	}
}
