package cli

import (
	"fmt"
	"io"
	"os"

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

func parseCommand(args []string) (*usagespec.Cli, error) {
	cli, err := usagespec.Parse(args)
	if err != nil {
		return nil, err
	}
	if cli.Run != nil {
		cli.Verbose, err = verbosityFromEnv(cli.Verbose)
	}
	return cli, err
}

func execute(args []string, exit *int, out io.Writer) error {
	if response, ok := argv.Respond(args, usagespec.Root, usagespec.HelpText, usagespec.Meta); ok {
		_, err := io.WriteString(out, response)
		return err
	}
	line, err := parseCommand(args)
	if e, ok := err.(*argv.Error); ok {
		meta := usagespec.HelpMeta
		meta.Version = Version
		if response, handled := argv.RenderRequest(e, meta, usagespec.Root, args, usagespec.HelpText); handled {
			_, err := io.WriteString(out, response)
			return err
		}
	}
	if err != nil {
		return err
	}
	switch {
	case line.Run != nil:
		return runAction(line.Run, line.Verbose, exit)
	case line.List != nil:
		return listAction(line.List.Config)
	case line.Migrate != nil:
		return migrateAction(line.Migrate.Path)
	case line.Cache != nil:
		if line.Cache.Clear == nil {
			position := argv.Walk(usagespec.Root, args)
			meta := usagespec.HelpMeta
			meta.Version = Version
			response, _ := argv.RenderRequest(&argv.Error{Code: argv.CodeHelp, Cmd: position.Cmd, Long: true}, meta, usagespec.Root, args, usagespec.HelpText)
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
