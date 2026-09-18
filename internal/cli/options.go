package cli

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jdx/usage/go/argv"

	"github.com/grafana/oats/internal/cli/usagespec"
)

// runCLIOptions adds application types to the generated spec's string values.
// Optional config/gcx/lgtm strings have no spec default: a nonempty value is an
// explicit override, even when it spells the application's usual default.
type runCLIOptions struct {
	*usagespec.RunCmd
	timeout, interval, absentTimeout, seedSettle time.Duration
	appPort, parallel                            int
}

func runOptionsFromCLI(raw *usagespec.RunCmd) (*runCLIOptions, error) {
	opts := &runCLIOptions{RunCmd: raw}
	for _, field := range []struct {
		name, value string
		target      *time.Duration
	}{
		{"timeout", raw.Timeout, &opts.timeout},
		{"interval", raw.Interval, &opts.interval},
		{"absent-timeout", raw.AbsentTimeout, &opts.absentTimeout},
		{"seed-settle", raw.SeedSettle, &opts.seedSettle},
	} {
		value, err := argv.Duration(field.name, field.value)
		if err != nil {
			return nil, err
		}
		*field.target = value
	}
	for _, field := range []struct {
		name, value string
		target      *int
	}{
		{"app-port", raw.AppPort, &opts.appPort},
		{"parallel", raw.Parallel, &opts.parallel},
	} {
		value, err := strconv.Atoi(field.value)
		if err != nil {
			return nil, fmt.Errorf("invalid --%s %q: %w", field.name, field.value, err)
		}
		*field.target = value
	}
	return opts, nil
}

func cacheDirectory(value string) string {
	if value == "" {
		return defaultCacheDir()
	}
	return value
}

// Temporary until shared count fallback semantics are agreed upstream:
// https://github.com/jdx/usage/discussions/1438.
// Native count flags only increment, so a positive count means CLI was given.
func verbosityFromEnv(count int) (int, error) {
	if count > 0 {
		return count, nil
	}
	value, set := os.LookupEnv("OATS_VERBOSE")
	if !set {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("invalid OATS_VERBOSE=%q: want a non-negative integer", value)
	}
	return count, nil
}
