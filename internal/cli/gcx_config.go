package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The remote fixture is configured in gcx, not in the OATS case. Keep the
// small subset needed for custom-check environment setup local to the CLI
// rather than coupling OATS to gcx's internal config package.
type gcxConfig struct {
	CurrentContext string                `yaml:"current-context"`
	Contexts       map[string]gcxContext `yaml:"contexts"`
}

type gcxContext struct {
	Grafana *gcxGrafana `yaml:"grafana"`
}

type gcxGrafana struct {
	Server string `yaml:"server"`
}

// remoteGrafanaURL reads the selected gcx context's Grafana server. An empty
// result means no gcx config was discoverable; this keeps remote runs that do
// not use custom checks compatible while avoiding the old, incorrect localhost
// fallback. An explicitly supplied GCX_CONFIG is strict so malformed CI
// configuration fails with an actionable error.
func remoteGrafanaURL(contextName string) (string, error) {
	paths, explicit := gcxConfigPaths()
	if len(paths) == 0 {
		return "", nil
	}

	merged := gcxConfig{Contexts: make(map[string]gcxContext)}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if explicit {
				return "", fmt.Errorf("read gcx config %s: %w", path, err)
			}
			continue
		}
		var cfg gcxConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			if explicit {
				return "", fmt.Errorf("parse gcx config %s: %w", path, err)
			}
			continue
		}
		if cfg.CurrentContext != "" {
			merged.CurrentContext = cfg.CurrentContext
		}
		for name, context := range cfg.Contexts {
			current := merged.Contexts[name]
			if context.Grafana != nil && context.Grafana.Server != "" {
				if current.Grafana == nil {
					current.Grafana = &gcxGrafana{}
				}
				current.Grafana.Server = context.Grafana.Server
			}
			merged.Contexts[name] = current
		}
	}

	if contextName == "" {
		contextName = merged.CurrentContext
	}
	context, ok := merged.Contexts[contextName]
	if !ok || context.Grafana == nil || strings.TrimSpace(context.Grafana.Server) == "" {
		if explicit {
			return "", fmt.Errorf("gcx context %q has no grafana.server", contextName)
		}
		return "", nil
	}

	server := strings.TrimRight(strings.TrimSpace(context.Grafana.Server), "/")
	u, err := url.Parse(server)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("gcx context %q has invalid grafana.server %q", contextName, server)
	}
	return server, nil
}

// gcxConfigPaths mirrors gcx's useful config locations: an explicit
// GCX_CONFIG, system config, the preferred user config, and a local .gcx.yaml.
// The returned order is low-to-high priority so later files can overlay a
// context's server value, matching gcx's layered configuration model.
func gcxConfigPaths() ([]string, bool) {
	if path := strings.TrimSpace(os.Getenv("GCX_CONFIG")); path != "" {
		return []string{path}, true
	}

	var paths []string
	for _, dir := range filepath.SplitList(configDirs()) {
		if dir != "" {
			paths = append(paths, filepath.Join(dir, "gcx", "config.yaml"))
		}
	}

	homeConfig := ""
	if home, err := os.UserHomeDir(); err == nil {
		homeConfig = filepath.Join(home, ".config", "gcx", "config.yaml")
	}
	xdgConfig := ""
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		xdgConfig = filepath.Join(dir, "gcx", "config.yaml")
	} else if homeConfig != "" {
		xdgConfig = homeConfig
	}
	// gcx prefers $HOME/.config over XDG_CONFIG_HOME when both exist.
	if homeConfig != "" && fileExists(homeConfig) {
		paths = append(paths, homeConfig)
	} else if xdgConfig != "" {
		paths = append(paths, xdgConfig)
	}

	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".gcx.yaml"))
	}

	var existing []string
	for _, path := range paths {
		if fileExists(path) && !contains(existing, path) {
			existing = append(existing, path)
		}
	}
	return existing, false
}

func configDirs() string {
	if dirs := strings.TrimSpace(os.Getenv("XDG_CONFIG_DIRS")); dirs != "" {
		return dirs
	}
	return "/etc/xdg"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
