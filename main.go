// gs (git-spice) is a command line tool for stacking Git branches.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mattn/go-isatty"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/komplete"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
)

// Version of git-spice.
// Set by goreleaser at build time.
var _version = "dev"

// _secretStash is the secret stash used by the application.
//
// This is overridden in tests to use a memory stash.
var _secretStash secret.Stash = new(secret.Keyring)

// TOOD:
// Adaptive secret stash that uses plain text
// if we don't have a keyring available.

var errNoPrompt = fmt.Errorf("not allowed to prompt for input")

func main() {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level: log.InfoLevel,
	})

	// Register supported forges.
	forge.Register(&github.Forge{Log: logger})

	styles := log.DefaultStyles()
	styles.Levels[log.DebugLevel] = ui.NewStyle().SetString("DBG").Bold(true)
	styles.Levels[log.InfoLevel] = ui.NewStyle().SetString("INF").Foreground(lipgloss.Color("10")).Bold(true) // green
	styles.Levels[log.WarnLevel] = ui.NewStyle().SetString("WRN").Foreground(lipgloss.Color("11")).Bold(true) // yellow
	styles.Levels[log.ErrorLevel] = ui.NewStyle().SetString("ERR").Foreground(lipgloss.Color("9")).Bold(true) // red
	styles.Levels[log.FatalLevel] = ui.NewStyle().SetString("FTL").Foreground(lipgloss.Color("9")).Bold(true) // red
	logger.SetStyles(styles)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		select {
		case <-sigc:
			logger.Info("Cleaning up. Press Ctrl-C again to exit immediately.")
			cancel()
		case <-ctx.Done():
		}
	}()

	var cmd mainCmd

	// Forges may register additional command line flags
	// by implementing CLIPlugin.
	forge.All(func(f forge.Forge) bool {
		if plugin := f.CLIPlugin(); plugin != nil {
			cmd.Plugins = append(cmd.Plugins, plugin)
		}
		return true
	})

	parser, err := kong.New(&cmd,
		kong.Name("gs"),
		kong.Description("gs (git-spice) is a command line tool for stacking Git branches."),
		kong.Bind(logger, &cmd.globalOptions),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindTo(_secretStash, (*secret.Stash)(nil)),
		kong.Vars{
			// Default to prompting only when the terminal is interactive.
			"defaultPrompt": strconv.FormatBool(isatty.IsTerminal(os.Stdin.Fd())),
		},
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.Help(func(options kong.HelpOptions, ctx *kong.Context) error {
			if err := kong.DefaultHelpPrinter(options, ctx); err != nil {
				return err
			}

			// For the help of the top-level command,
			// print a note about shorthand aliases.
			if len(ctx.Command()) == 0 {
				_, _ = fmt.Fprint(ctx.Stdout,
					"\n",
					"Aliases can be combined to form shorthands for commands. For example:\n",
					"  gs bc => gs branch create\n",
					"  gs cc => gs commit create\n",
				)
			}

			return nil
		}),
	)
	if err != nil {
		panic(err)
	}

	// The default help flag text has a period at the end,
	// which doesn't match the rest of our help text.
	// Remove the period and place it in the same group
	// as the other global flags.
	if help := parser.Model.HelpFlag; help != nil {
		help.Help = "Show help for the command"
		help.Group = &kong.Group{
			Key:   "globals",
			Title: "Global Flags:",
		}
	}

	shorthands := make(shorthands)

	// For each leaf subcommand, define a combined shorthand alias.
	// For example, if the command was "branch (b) create (c)",
	// the shorthand would be "bc".
	// For commands with multiple aliases, only the first is used.
	for _, n := range parser.Model.Leaves(false) {
		if n.Type != kong.CommandNode || len(n.Aliases) == 0 {
			continue
		}

		var fragments []string
		for c := n; c != nil && c.Type == kong.CommandNode; c = c.Parent {
			if len(c.Aliases) < 1 {
				panic(fmt.Sprintf("expected an alias for %q (%v)", c.Name, c.Path()))
			}
			fragments = append(fragments, c.Aliases[0])
		}
		if len(fragments) < 2 {
			// If the command is already a single word, don't add an alias.
			continue
		}

		slices.Reverse(fragments)
		short := strings.Join(fragments, "")
		if other, ok := shorthands[short]; ok {
			panic(fmt.Sprintf("shorthand %q for %v is already in use by %v", short, n.Path(), other.Expanded))
		}
		// TODO: check if shorthand conflicts with any other aliases.

		shorthands[short] = shorthand{
			Expanded: fragments,
			Command:  n,
		}
	}

	args := os.Args[1:]
	if len(args) > 0 {
		if short, ok := shorthands[args[0]]; ok {
			// TODO: Replace first non-flag argument instead.
			args = slices.Replace(args, 0, 1, short.Expanded...)
		}
	}

	komplete.Run(parser,
		komplete.WithTransformCompleted(func(args []string) []string {
			if len(args) > 0 {
				if short, ok := shorthands[args[0]]; ok {
					args = slices.Replace(args, 0, 1, short.Expanded...)
				}
			}
			return args
		}),
		komplete.WithPredictor("branches", komplete.PredictFunc(predictBranches)),
		komplete.WithPredictor("trackedBranches", komplete.PredictFunc(predictTrackedBranches)),
		komplete.WithPredictor("remotes", komplete.PredictFunc(predictRemotes)),
		komplete.WithPredictor("dirs", komplete.PredictFunc(predictDirs)),
		komplete.WithPredictor("forges", komplete.PredictFunc(predictForges)),
	)

	kctx, err := parser.Parse(args)
	if err != nil {
		logger.Fatalf("gs: %v", err)
	}

	if err := kctx.Run(shorthands); err != nil {
		logger.Fatalf("gs: %v", err)
	}
}

type shorthand struct {
	Expanded []string
	Command  *kong.Node
}

type shorthands map[string]shorthand

type globalOptions struct {
	// Flags that are not accessed directly by command implementations:

	Version versionFlag        `help:"Print version information and quit"`
	Verbose bool               `short:"v" help:"Enable verbose output" env:"GIT_SPICE_VERBOSE"`
	Dir     kong.ChangeDirFlag `short:"C" placeholder:"DIR" help:"Change to DIR before doing anything" predictor:"dirs"`

	// Flags that are accessed directly:

	Prompt bool `name:"prompt" negatable:"" default:"${defaultPrompt}" help:"Whether to prompt for missing information"`
}

type mainCmd struct {
	kong.Plugins
	globalOptions `group:"globals"`

	Shell shellCmd `cmd:"" group:"Shell"`
	Auth  authCmd  `cmd:"" group:"Authentication"`

	Repo repoCmd `cmd:"" aliases:"r" group:"Repository"`
	Log  logCmd  `cmd:"" aliases:"l" group:"Log"`

	Stack     stackCmd     `cmd:"" aliases:"s" group:"Stack"`
	Upstack   upstackCmd   `cmd:"" aliases:"us" group:"Stack"`
	Downstack downstackCmd `cmd:"" aliases:"ds" group:"Stack"`

	Branch branchCmd `cmd:"" aliases:"b" group:"Branch"`
	Commit commitCmd `cmd:"" aliases:"c" group:"Commit"`

	Rebase rebaseCmd `cmd:"" aliases:"rb" group:"Rebase"`

	// Navigation
	Up     upCmd     `cmd:"" aliases:"u" group:"Navigation" help:"Move up one branch"`
	Down   downCmd   `cmd:"" aliases:"d" group:"Navigation" help:"Move down one branch"`
	Top    topCmd    `cmd:"" aliases:"U" group:"Navigation" help:"Move to the top of the stack"`
	Bottom bottomCmd `cmd:"" aliases:"D" group:"Navigation" help:"Move to the bottom of the stack"`
	Trunk  trunkCmd  `cmd:"" group:"Navigation" help:"Move to the trunk branch"`

	// Hidden commands:
	DumpMD dumpMarkdownCmd `name:"dumpmd" hidden:"" cmd:"" help:"Dump a Markdown reference to stdout and quit"`
}

func (cmd *mainCmd) AfterApply(kctx *kong.Context, logger *log.Logger) error {
	if cmd.Verbose {
		logger.SetLevel(log.DebugLevel)
	}

	return nil
}
