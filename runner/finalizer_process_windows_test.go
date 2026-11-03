//go:build windows

package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsFinalizerProcessTree(t *testing.T) {
	for _, mode := range []string{"parent-exits", "parent-waits"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "child-ran")
			ready := filepath.Join(dir, "child-started")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsFinalizerProcessHelper$")
			cmd.Env = windowsFinalizerHelperEnv(mode, marker, ready)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			cmd.WaitDelay = fixtureFinalizerWaitDelay

			stop, cleanup, err := startFinalizerProcess(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = stop()
				_ = cleanup()
			}()
			waitForFile(t, ready)
			if mode == "parent-waits" {
				cancel()
			}
			waitErr := cmd.Wait()
			if mode == "parent-waits" && waitErr == nil {
				t.Fatal("cancelled parent exited successfully")
			}
			if waitErr != nil {
				_ = stop()
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * time.Second)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("descendant survived finalizer cleanup: stat error = %v", err)
			}
		})
	}
}

func TestWindowsFinalizerProcessHelper(t *testing.T) {
	mode := os.Getenv("OATS_WINDOWS_FINALIZER_HELPER")
	switch mode {
	case "child":
		_ = os.WriteFile(os.Getenv("OATS_WINDOWS_FINALIZER_READY"), []byte("started"), 0o600)
		time.Sleep(time.Second)
		_ = os.WriteFile(os.Getenv("OATS_WINDOWS_FINALIZER_MARKER"), []byte("ran"), 0o600)
		os.Exit(0)
	case "parent-exits", "parent-waits":
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsFinalizerProcessHelper$")
		child.Env = windowsFinalizerHelperEnv("child", os.Getenv("OATS_WINDOWS_FINALIZER_MARKER"), os.Getenv("OATS_WINDOWS_FINALIZER_READY"))
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		waitForHelperFile(os.Getenv("OATS_WINDOWS_FINALIZER_READY"))
		if mode == "parent-waits" {
			time.Sleep(30 * time.Second)
		}
		os.Exit(0)
	}
}

func waitForHelperFile(path string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(2)
}

func windowsFinalizerHelperEnv(mode, marker, ready string) []string {
	return append(os.Environ(), "OATS_WINDOWS_FINALIZER_HELPER="+mode,
		"OATS_WINDOWS_FINALIZER_MARKER="+marker, "OATS_WINDOWS_FINALIZER_READY="+ready)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper did not signal startup: %s", path)
}
