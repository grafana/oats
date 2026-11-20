package compose

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"
)

// serviceProfiles returns profiles for a service from the Compose source
// files. This targeted source-level lookup is not a full Compose config
// evaluator; podman-compose filters profiles literally and does not support
// the wildcard profile.
func serviceProfiles(ctx context.Context, files, env []string, service string) ([]string, error) {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	profiles := make([]string, 0)
	seen := make(map[string]struct{})
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		var config struct {
			Services map[string]struct {
				Profiles []string `yaml:"profiles"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		for _, profile := range config.Services[service].Profiles {
			profile = os.Expand(profile, func(key string) string {
				if value, ok := values[key]; ok {
					return value
				}
				if value, ok := os.LookupEnv(key); ok {
					return value
				}
				return "$" + key
			})
			if strings.Contains(profile, "$") {
				return nil, fmt.Errorf("profile %q in %s contains unsupported or unresolved interpolation", profile, file)
			}
			if profile == "" {
				return nil, fmt.Errorf("profile in %s resolved to an empty value", file)
			}
			if _, ok := seen[profile]; !ok {
				seen[profile] = struct{}{}
				profiles = append(profiles, profile)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}
