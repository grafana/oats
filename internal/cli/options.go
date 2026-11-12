package cli

import (
	"fmt"
	"os"
	"strconv"
)

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
