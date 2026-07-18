package container

import (
	"os"
	"path/filepath"
	"runtime"
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
