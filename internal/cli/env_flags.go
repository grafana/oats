package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

const flagEnvPrefix = "OATS_"

// applyEnvFlags applies OATS_* environment variables to flags that were not
// explicitly set on the command line. This keeps environment configuration
// useful in CI while preserving the usual CLI-over-environment precedence.
func applyEnvFlags(fs *pflag.FlagSet) error {
	var applyErr error
	fs.VisitAll(func(flag *pflag.Flag) {
		if applyErr != nil || flag.Changed {
			return
		}
		envName := flagEnvName(flag.Name)
		raw, ok := os.LookupEnv(envName)
		if !ok {
			return
		}

		if err := flag.Value.Set(raw); err != nil {
			applyErr = fmt.Errorf("invalid %s=%q for --%s: %w", envName, raw, flag.Name, err)
			return
		}
		// Treat environment-supplied values like explicit flags so code that
		// needs to distinguish defaults from configuration sees the same result.
		flag.Changed = true
	})
	return applyErr
}

func flagEnvName(flagName string) string {
	return flagEnvPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}
