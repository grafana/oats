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

type commandLine struct {
	*usagespec.Cli
	run *runCLIOptions
}

func parseCommand(args []string) (*commandLine, error) {
	cli, err := usagespec.Parse(args)
	if err != nil {
		return nil, err
	}
	// Usage keeps an empty invocation (or only global flags) on the parent.
	// OATs runs tests in that case; parse the same options in the run scope so
	// defaults and environment resolution still belong to the generated parser.
	if cli.Run == nil && cli.List == nil && cli.Migrate == nil && cli.Cache == nil &&
		cli.VersionCmd == nil && cli.Usage == nil && cli.Completion == nil {
		cli, err = usagespec.Parse(append([]string{"run"}, args...))
		if err != nil {
			return nil, err
		}
	}
	line := &commandLine{Cli: cli}
	if cli.Run != nil {
		line.run, err = runOptionsFromCLI(cli.Run)
		if err != nil {
			return nil, err
		}
		cli.Verbose, err = verbosityFromEnv(cli.Verbose)
	}
	return line, err
}

func execute(args []string, exit *int, out io.Writer) error {
	if response, ok := argv.Respond(args, usagespec.Root, usagespec.HelpText, usagespec.Meta); ok {
		_, err := io.WriteString(out, response)
		return err
	}
	line, err := parseCommand(args)
	if e, ok := err.(*argv.Error); ok {
		switch e.Code {
		case argv.CodeHelp:
			return printHelp(out, commandChain(usagespec.Root, e.Cmd), e.Long)
		case argv.CodeVersion:
			_, err := fmt.Fprintln(out, Version)
			return err
		}
	}
	if err != nil {
		return err
	}
	switch {
	case line.Run != nil:
		return runAction(line.run, line.Verbose, exit)
	case line.List != nil:
		return listAction(line.List.Config)
	case line.Migrate != nil:
		return migrateAction(line.Migrate.Path)
	case line.Cache != nil:
		if line.Cache.Clear == nil {
			return printHelp(out, argv.Walk(usagespec.Root, args).Chain, true)
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

// Temporary until the upstream Walk help-topic fix is available:
// https://github.com/zeitlinger/usage/tree/fix/go-help-topic-chain.
// OATs has a tree with no shared command nodes, so target lookup is unambiguous.
func commandChain(root, target *argv.Command) []*argv.Command {
	if root == target {
		return []*argv.Command{root}
	}
	for _, child := range root.Subcommands {
		if chain := commandChain(child, target); chain != nil {
			return append([]*argv.Command{root}, chain...)
		}
	}
	return nil
}

func printHelp(out io.Writer, chain []*argv.Command, long bool) error {
	path := make([]string, len(chain))
	for i, cmd := range chain {
		path[i] = cmd.Name
	}
	meta := usagespec.HelpMeta
	meta.Version = Version
	var page string
	if long {
		page = argv.LongHelp(meta, path, chain, usagespec.HelpText)
	} else {
		page = argv.ShortHelp(meta, path, chain, usagespec.HelpText)
	}
	_, err := io.WriteString(out, page)
	return err
}
