package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGCX_ExecuteCapturesStdout(t *testing.T) {
	g := &GCX{Binary: "/bin/sh"}
	r, err := g.Execute(context.Background(), "-c", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", r.ExitCode)
	}
	if strings.TrimSpace(r.Stdout) != "hello" {
		t.Errorf("Stdout: got %q, want %q", r.Stdout, "hello\n")
	}
}

func TestGCX_ExecuteCapturesStderr(t *testing.T) {
	g := &GCX{Binary: "/bin/sh"}
	r, err := g.Execute(context.Background(), "-c", "echo oops >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(r.Stderr) != "oops" {
		t.Errorf("Stderr: got %q, want %q", r.Stderr, "oops\n")
	}
}

func TestGCX_NonZeroExitIsNotAnError(t *testing.T) {
	g := &GCX{Binary: "/bin/sh"}
	r, err := g.Execute(context.Background(), "-c", "exit 7")
	if err != nil {
		t.Fatalf("non-zero exit should not return a Go error, got %v", err)
	}
	if r.ExitCode != 7 {
		t.Errorf("ExitCode: got %d, want 7", r.ExitCode)
	}
}

func TestGCX_MissingBinaryIsAnError(t *testing.T) {
	g := &GCX{Binary: ""}
	_, err := g.Execute(context.Background(), "anything")
	if err == nil {
		t.Fatal("expected an error for empty binary path")
	}
}

func TestGCX_PrependsContextFlag(t *testing.T) {
	g := &GCX{Binary: "/bin/sh", Context: "my-ctx"}
	r, err := g.Execute(context.Background(), "-c", `printf "%s\n" "$@"`, "_", "verb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The command we asked sh to run echoes the positional args. With our
	// Context set, gcx invocations gain a leading "--context my-ctx" pair.
	// Since we're using /bin/sh as a stand-in for gcx, we observe the args
	// by way of the recorded Command.
	want := []string{"/bin/sh", "--context", "my-ctx", "-c", `printf "%s\n" "$@"`, "_", "verb"}
	if len(r.Command) != len(want) {
		t.Fatalf("Command len: got %d, want %d (%v)", len(r.Command), len(want), r.Command)
	}
	for i := range want {
		if r.Command[i] != want[i] {
			t.Errorf("Command[%d]: got %q, want %q", i, r.Command[i], want[i])
		}
	}
}

func TestGCX_TimeoutKillsLongProcess(t *testing.T) {
	g := &GCX{Binary: "/bin/sh", Timeout: 50 * time.Millisecond}
	_, err := g.Execute(context.Background(), "-c", "sleep 5")
	if err == nil {
		t.Fatal("expected an error when child overruns the timeout")
	}
}
