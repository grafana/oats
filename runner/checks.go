// Compose-log and custom-check assertions: shelling out to docker compose
// and user-provided scripts, plus the small helpers they depend on.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/grafana/oats/assert"
	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/report"
	"github.com/grafana/oats/testhelpers/container"
	"github.com/grafana/oats/wait"
)

func (r *Runner) runComposeLogCheck(ctx context.Context, c *casefile.Case, msg string) bool {
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		ok, err := searchComposeLogs(ctx, r.endpoint.CustomCheckEnv, msg)
		if err != nil {
			return []assert.Failure{{Rule: "compose-logs", Detail: err.Error()}}
		}
		if !ok {
			return []assert.Failure{{Rule: "compose-logs", Detail: fmt.Sprintf("missing %q", msg)}}
		}
		return nil
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:   report.EventAssertFail,
			Case:   c.Name,
			Source: c.SourcePath,
			Msg:    f.Error(),
			Cmd:    "compose-logs",
		})
	}
	return false
}

func (r *Runner) runCustomCheck(ctx context.Context, c *casefile.Case, chk *casefile.CustomCheck) bool {
	dir := "."
	if c.SourcePath != "" {
		dir = filepath.Dir(c.SourcePath)
	}
	run := func() []assert.Failure {
		if err := r.driveInputs(c); err != nil {
			return []assert.Failure{{Rule: "input", Detail: err.Error()}}
		}
		deadlineCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
		defer cancel()

		cmd, cleanup, err := customCheckCommand(deadlineCtx, dir, chk.Script, r.endpoint.CustomCheckEnv)
		if err != nil {
			return []assert.Failure{{Rule: "custom-check-setup", Detail: err.Error()}}
		}
		defer cleanup()

		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return []assert.Failure{{Rule: "custom-check", Detail: err.Error() + "\n" + trimOutput(out.String())}}
		}
		return nil
	}

	result := wait.Until[assert.Failure](ctx, wait.Options{Timeout: r.opts.Timeout, Interval: r.caseInterval(c)}, run)
	if result.OK {
		return true
	}
	for _, f := range result.LastFailures {
		r.reporter.Emit(report.Event{
			Type:   report.EventAssertFail,
			Case:   c.Name,
			Source: c.SourcePath,
			Msg:    f.Error(),
			Cmd:    "custom-check",
		})
	}
	return false
}

func customCheckCommand(ctx context.Context, dir, script string, extraEnv []string) (*exec.Cmd, func(), error) {
	cleanup := func() {}
	if strings.TrimSpace(script) == "" {
		return nil, cleanup, fmt.Errorf("empty script")
	}
	if looksLikeInlineScript(script) {
		f, err := os.CreateTemp("", "oats-custom-check-*.sh")
		if err != nil {
			return nil, cleanup, err
		}
		if _, err := f.WriteString(script); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		if err := os.Chmod(f.Name(), 0o700); err != nil {
			_ = os.Remove(f.Name())
			return nil, cleanup, err
		}
		cleanup = func() { _ = os.Remove(f.Name()) }
		cmd := exec.CommandContext(ctx, f.Name())
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), extraEnv...)
		return cmd, cleanup, nil
	}
	cmd := exec.CommandContext(ctx, resolveCustomCheckPath(dir, script))
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), extraEnv...)
	return cmd, cleanup, nil
}

func searchComposeLogs(ctx context.Context, extraEnv []string, needle string) (bool, error) {
	if envValue(extraEnv, "OATS_FIXTURE_TYPE") != "compose" {
		return false, fmt.Errorf("compose-logs requires a compose fixture")
	}
	engine := container.Docker
	if raw := envValue(extraEnv, "OATS_CONTAINER_RUNTIME"); raw != "" {
		parsed, err := container.Parse(raw)
		if err != nil {
			return false, err
		}
		if parsed == container.Auto {
			parsed, err = container.Resolve(raw)
			if err != nil {
				return false, err
			}
		}
		engine = parsed
	}
	cmd := exec.CommandContext(ctx, engine.Binary(), engine.ComposeArgs("logs", "--no-color")...)
	cmd.Env = append(cmd.Environ(), extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("%s compose logs: %w\n%s", engine, err, trimOutput(out.String()))
	}
	return strings.Contains(out.String(), needle), nil
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

func looksLikeInlineScript(script string) bool {
	trimmed := strings.TrimSpace(script)
	return strings.Contains(trimmed, "\n") || strings.HasPrefix(trimmed, "#!")
}

func resolveCustomCheckPath(dir, script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		return script
	}
	if filepath.IsAbs(script) {
		return script
	}
	if strings.ContainsRune(script, os.PathSeparator) {
		joined := filepath.Clean(filepath.Join(dir, script))
		if abs, err := filepath.Abs(joined); err == nil {
			return abs
		}
		return joined
	}
	return script
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}
