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
	gcxExplicit := fs.Changed("gcx")
	gcxVersionExplicit := fs.Changed("gcx-version")
	if !gcxExplicit && !gcxVersionExplicit {
		if _, hasGCX := os.LookupEnv(flagEnvName("gcx")); hasGCX {
			if _, hasGCXVersion := os.LookupEnv(flagEnvName("gcx-version")); hasGCXVersion {
				return fmt.Errorf("cannot set both %s and %s; choose one", flagEnvName("gcx"), flagEnvName("gcx-version"))
			}
		}
	}
	fs.VisitAll(func(flag *pflag.Flag) {
		if applyErr != nil || flag.Changed {
			return
		}
		envName := flagEnvName(flag.Name)
		raw, ok := os.LookupEnv(envName)
		if !ok {
			return
		}

		// An explicit value for one of the mutually exclusive gcx flags wins over
		// the environment value of the other flag. Warn because this is a
		// cross-flag conflict rather than the normal same-flag precedence rule.
		if flag.Name == "gcx" && gcxVersionExplicit {
			warnIgnoredEnv(flagEnvName(flag.Name), "gcx-version")
			return
		}
		if flag.Name == "gcx-version" && gcxExplicit {
			warnIgnoredEnv(flagEnvName(flag.Name), "gcx")
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

func warnIgnoredEnv(envName, flagName string) {
	fmt.Fprintf(os.Stderr, "warning: %s is ignored because --%s was provided\n", envName, flagName)
}
