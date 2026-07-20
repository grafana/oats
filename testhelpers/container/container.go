// Package container describes the host container engine used by fixture
// adapters. It intentionally does not own Compose or Kubernetes lifecycle;
// those are separate adapters with different capabilities.
package container

import (
	"fmt"
	"os/exec"
	"strings"
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
	if engine == Podman {
		// podman compose delegates to an external provider. Probe it during
		// auto-selection so an installed Podman without podman-compose (or a
		// configured provider) falls back to Docker instead of failing later.
		if err := exec.Command(engine.Binary(), "compose", "version").Run(); err != nil {
			return fmt.Errorf("compose provider unavailable: %w", err)
		}
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
