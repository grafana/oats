// Package engine executes gcx CLI commands and returns the result.
//
// engine is the seam between OATS and gcx: OATS no longer talks HTTP to Tempo,
// Loki, Prometheus, or Pyroscope. It builds a gcx command, runs it through an
// Executor, and hands the captured output to the assert package.
//
// The Executor interface keeps tests fast — production wiring uses GCX, which
// shells out via os/exec; tests use a stub that returns canned Results.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Result captures everything a downstream assertion might need from a single
// gcx invocation. ExitCode is captured separately from any Go-level error so
// callers can distinguish "process did not start" from "process ran and
// reported a non-zero status."
type Result struct {
	Command  []string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor runs a gcx invocation and returns its Result.
//
// Implementations must never return a non-nil error for a non-zero exit code —
// that is conveyed via Result.ExitCode. A non-nil error means the process
// could not be launched (missing binary, context cancelled before start,
// permission denied, etc.).
type Executor interface {
	Execute(ctx context.Context, args ...string) (*Result, error)
}

// GCX is the production Executor. It shells out to a gcx binary on disk.
type GCX struct {
	// Binary is the path to the gcx executable. Required.
	Binary string

	// Context is the value passed via --context, prepended to every invocation
	// when non-empty. OATS sets this per suite so that a single binary install
	// can drive multiple Grafana endpoints.
	Context string

	// Config is an optional path passed via --config before every invocation.
	// OATS uses this for local LGTM fixtures so gcx can query local datasources
	// without consumer-managed wrapper scripts or ambient config state.
	Config string

	// Env supplements os.Environ() for the child process. Use to pass HOME /
	// XDG_CONFIG_HOME for config isolation.
	Env []string

	// Timeout caps a single invocation. Zero means no per-invocation timeout —
	// the caller's context deadline still applies.
	Timeout time.Duration
}

// Execute runs gcx with args. When set, GCX.Config and GCX.Context are
// prepended automatically (as "--config <cfg> --context <ctx>"), so callers
// pass just the verb and its flags (e.g. "traces", "search", "--since=10m",
// "{ ... }").
func (g *GCX) Execute(ctx context.Context, args ...string) (*Result, error) {
	if g.Binary == "" {
		return nil, fmt.Errorf("engine: gcx binary path is empty")
	}

	full := args
	if g.Context != "" {
		full = append([]string{"--context", g.Context}, full...)
	}
	if g.Config != "" {
		full = append([]string{"--config", g.Config}, full...)
	}

	if g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, g.Binary, full...)
	if len(g.Env) > 0 {
		cmd.Env = append(cmd.Environ(), g.Env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Command:  append([]string{g.Binary}, full...),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	// Timeout / cancellation takes precedence over exit code so callers can
	// distinguish "gcx ran and failed" from "gcx was killed before it could
	// answer."
	if ctx.Err() != nil {
		return result, fmt.Errorf("engine: gcx invocation aborted: %w", ctx.Err())
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit — surface via ExitCode, not via Go error.
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// Process did not start, was killed by the OS, permission denied, etc.
		return result, fmt.Errorf("engine: gcx invocation failed: %w", err)
	}

	return result, nil
}
