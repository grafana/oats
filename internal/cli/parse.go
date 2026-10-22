package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jdx/usage/go/argv"

	"github.com/grafana/oats/cache"
	"github.com/grafana/oats/internal/cli/usagespec"
)

// Run maps command failures to exit 2 and failed assertions to exit 1.
func Run() int {
	var exit int
	if err := execute(os.Args[1:], &exit, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if exit == 0 {
			exit = 2
		}
	}
	return exit
}

// helpText returns a private copy so build-aware help never mutates generated
// parser metadata shared by concurrent CLI invocations.
func helpText() argv.HelpTable {
	text := append(argv.HelpTable(nil), usagespec.HelpText...)
	if MinimumGCXVersion == "" {
		return text
	}
	for i := range text {
		if text[i].Key == usagespec.FlagRunGcxVersion {
			text[i].Short = fmt.Sprintf("download and use this gcx release (minimum supported by this build: %s)", MinimumGCXVersion)
			text[i].Long = text[i].Short
			break
		}
	}
	return text
}

func parseCommand(args []string) (*usagespec.Cli, error) {
	cli, err := usagespec.Parse(args)
	if err != nil {
		return nil, err
	}
	if hasAttachedVerboseValue(args) {
		return nil, fmt.Errorf("the --verbose count flag does not accept an attached value; use repeated -v flags or OATS_VERBOSE")
	}
	if cli.Run != nil {
		cli.Verbose, err = verbosityFromEnv(cli.Verbose)
	}
	return cli, err
}

// hasAttachedVerboseValue rejects count assignments the Usage runtime silently
// treats as a single occurrence. Parse runs first, so any matching token before
// `--` is a flag rather than an option operand; the generated Usage parser
// remains the source of truth for command parsing.
func hasAttachedVerboseValue(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "--verbose=") {
			return true
		}
	}
	return false
}

func execute(args []string, exit *int, out io.Writer) error {
	help := helpText()
	if response, ok := argv.Respond(args, usagespec.Root, help, usagespec.Meta); ok {
		_, err := io.WriteString(out, response)
		return err
	}
	line, err := parseCommand(args)
	if e, ok := err.(*argv.Error); ok {
		meta := usagespec.HelpMeta
		meta.Version = Version
		if response, handled := argv.RenderRequest(e, meta, usagespec.Root, args, help); handled {
			_, err := io.WriteString(out, response)
			return err
		}
	}
	if err != nil {
		return err
	}
	switch {
	case line.Run != nil:
		return runAction(line.Run, line.Verbose, exit, out)
	case line.List != nil:
		return listAction(line.List.Config, out)
	case line.Migrate != nil:
		return migrateAction(line.Migrate.Path, out)
	case line.Cache != nil:
		if line.Cache.Clear == nil {
			position := argv.Walk(usagespec.Root, args)
			meta := usagespec.HelpMeta
			meta.Version = Version
			response, _ := argv.RenderRequest(&argv.Error{Code: argv.CodeHelp, Cmd: position.Cmd, Long: true}, meta, usagespec.Root, args, help)
			_, err := io.WriteString(out, response)
			return err
		}
		dir := cacheDirectory(line.Cache.Clear.CacheDir)
		store, err := cache.New(dir, 0, nil)
		if err != nil {
			return err
		}
		if err := store.Clear(); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "cache cleared:", dir)
		return err
	case line.VersionCmd != nil:
		_, err := fmt.Fprintln(out, Version)
		return err
	case line.Usage != nil:
		_, err := io.WriteString(out, usagespec.Spec)
		return err
	case line.Completion != nil:
		shell, _ := argv.ShellNamed(line.Completion.Shell) // choices validated by Parse
		_, err := io.WriteString(out, argv.Script("oats", shell))
		return err
	default:
		return fmt.Errorf("no command selected")
	}
}
