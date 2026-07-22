// Package container describes the host container engine used by fixture
// adapters. It intentionally does not own Compose or Kubernetes lifecycle;
// those are separate adapters with different capabilities.
package container

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Engine is a supported host container engine.
type Engine string

const (
	// Auto prefers Podman and falls back to Docker when the user did not select
	// an engine explicitly.
	Auto   Engine = "auto"
	Docker Engine = "docker"
	Podman Engine = "podman"

	composeProbeTimeout = 5 * time.Second
)

// Parse validates a requested engine name. An empty value means Auto.
func Parse(value string) (Engine, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(Auto):
		return Auto, nil
	case string(Docker):
		return Docker, nil
	case string(Podman):
		return Podman, nil
	default:
		return "", fmt.Errorf("unsupported container runtime %q (want auto, docker, or podman)", value)
	}
}

// Resolve selects an installed engine. Explicit selections are never silently
// replaced by another engine; Auto is the only policy that falls back.
func Resolve(value string) (Engine, error) {
	requested, err := Parse(value)
	if err != nil {
		return "", err
	}
	if requested != Auto {
		if err := available(requested); err != nil {
			return "", fmt.Errorf("container runtime %s is not available: %w", requested, err)
		}
		return requested, nil
	}
	// Try engines in preference order.
	for _, candidate := range []Engine{Podman, Docker} {
		if available(candidate) == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no supported container runtime found on PATH (tried podman, docker)")
}

func available(engine Engine) error {
	if _, err := exec.LookPath(engine.Binary()); err != nil {
		return err
	}
	// Podman compose delegates to an external provider, while Docker compose
	// may be an optional CLI plugin. Probe both during selection so an engine
	// without a usable Compose implementation fails with an actionable error
	// instead of failing later during fixture startup.
	if output, err := runComposeProbe(engine, "version"); err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("compose unavailable: %s: %w", message, err)
		}
		return fmt.Errorf("compose unavailable: %w", err)
	}
	// `compose version` only checks the client/provider. A provider can still
	// be unable to reach the engine API, which is common when Podman is
	// installed but its service socket is not running. Probe a harmless empty
	// Compose project so auto mode can try the next engine before fixture
	// startup.
	probeDir, err := os.MkdirTemp("", "oats-runtime-probe-")
	if err != nil {
		return fmt.Errorf("create compose probe directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(probeDir) }()
	probePath := filepath.Join(probeDir, "compose.yml")
	if err := os.WriteFile(probePath, []byte("services:\n  oats-runtime-probe:\n    image: scratch\n"), 0o600); err != nil {
		return fmt.Errorf("write compose probe: %w", err)
	}

	if output, err := runComposeProbe(engine, "-f", probePath, "ps"); err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("container engine unavailable: %s: %w", message, err)
		}
		return fmt.Errorf("container engine unavailable: %w", err)
	}
	return nil
}

func runComposeProbe(engine Engine, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), composeProbeTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, engine.Binary(), append([]string{"compose"}, args...)...).CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("compose probe timed out after %s: %w", composeProbeTimeout, ctx.Err())
	}
	return output, err
}

// Binary returns the executable used by the engine.
func (e Engine) Binary() string {
	switch e {
	case Podman:
		return "podman"
	case Docker:
		return "docker"
	default:
		return ""
	}
}

// ComposeArgs prefixes args with the Compose subcommand used by Docker and
// Podman. Podman delegates Compose to a provider, so provider compatibility is
// checked by the fixture startup rather than assumed here.
func (e Engine) ComposeArgs(args ...string) []string {
	return append([]string{"compose"}, args...)
}
