// Package container describes the host container engine used by fixture
// adapters. It intentionally does not own Compose or Kubernetes lifecycle;
// those are separate adapters with different capabilities.
package container

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	if err := exec.Command(engine.Binary(), "compose", "version").Run(); err != nil {
		return fmt.Errorf("compose unavailable: %w", err)
	}
	// `compose version` only checks the client/provider. A provider can still
	// be unable to reach the engine API, which is common when Podman is
	// installed but its service socket is not running. Probe a harmless empty
	// Compose project so auto mode can try the next engine before fixture
	// startup.
	probe, err := os.CreateTemp("", "oats-runtime-probe-*.compose.yml")
	if err != nil {
		return fmt.Errorf("create compose probe: %w", err)
	}
	probePath := probe.Name()
	defer func() { _ = os.Remove(probePath) }()
	if _, err := probe.WriteString("services:\n  oats-runtime-probe:\n    image: scratch\n"); err != nil {
		_ = probe.Close()
		return fmt.Errorf("write compose probe: %w", err)
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close compose probe: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, engine.Binary(), "compose", "-f", probePath, "ps").CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("container engine unavailable: %s: %w", message, err)
		}
		return fmt.Errorf("container engine unavailable: %w", err)
	}
	return nil
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
