// gs is a command line tool to manage a stack of GitHub pull requests.
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
	"go.abhg.dev/gs/internal/gh"
	"golang.org/x/oauth2"
)

var _version = "dev"

var errNoPrompt = fmt.Errorf("not allowed to prompt for input")

func main() {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level: log.InfoLevel,
	})

	styles := log.DefaultStyles()
	styles.Levels[log.DebugLevel] = lipgloss.NewStyle().SetString("DBG").Bold(true)
	styles.Levels[log.InfoLevel] = lipgloss.NewStyle().SetString("INF").Foreground(lipgloss.Color("10")).Bold(true) // green
	styles.Levels[log.WarnLevel] = lipgloss.NewStyle().SetString("WRN").Foreground(lipgloss.Color("11")).Bold(true) // yellow
	styles.Levels[log.ErrorLevel] = lipgloss.NewStyle().SetString("ERR").Foreground(lipgloss.Color("9")).Bold(true) // red
	styles.Levels[log.FatalLevel] = lipgloss.NewStyle().SetString("FTL").Foreground(lipgloss.Color("9")).Bold(true) // red
	logger.SetStyles(styles)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		select {
		case <-sigc:
			log.Info("Cleaning up. Press Ctrl-C again to exit immediately.")
			cancel()
		case <-ctx.Done():
		}
	}()

	var cmd mainCmd
	parser, err := kong.New(&cmd,
		kong.Name("gs"),
		kong.Description("gs is a command line tool to manage stacks of GitHub pull requests."),
		kong.Bind(logger, &cmd.globalOptions),
		kong.BindTo(ctx, (*context.Context)(nil)),
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

	shorthands := map[string][]string{
		"can": {"commit", "amend", "--no-edit"},
	}

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
		shorthand := strings.Join(fragments, "")
		if other, ok := shorthands[shorthand]; ok {
			panic(fmt.Sprintf("shorthand %q for %v is already in use by %v", shorthand, n.Path(), other))
		}
		// TODO: check if shorthand conflicts with any other aliases.

		shorthands[shorthand] = fragments
	}

	args := os.Args[1:]
	if len(args) > 0 {
		if path, ok := shorthands[args[0]]; ok {
			args = slices.Replace(args, 0, 1, path...)
		}
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		logger.Fatalf("gs: %v", err)
	}

	if err := kctx.Run(); err != nil {
		logger.Fatalf("gs: %v", err)
	}
}

type globalOptions struct {
	GitHubToken  string `name:"github-token" placeholder:"TOKEN" env:"GITHUB_TOKEN" help:"GitHub API token"`
	GithubAPIURL string `name:"github-api-url" placeholder:"URL" env:"GITHUB_API_URL" help:"Base URL for GitHub API requests"`

	Prompt bool `name:"prompt" negatable:"" default:"${defaultPrompt}" help:"Whether to prompt for missing information"`
}

type mainCmd struct {
	// Flags with side effects whose values are never accesssed directly.
	Version versionFlag        `help:"Print version information and quit"`
	Verbose bool               `short:"v" help:"Enable verbose output" env:"GS_VERBOSE"`
	Dir     kong.ChangeDirFlag `short:"C" placeholder:"DIR" help:"Change to DIR before doing anything"`

	globalOptions

	Repo    repoCmd    `cmd:"" aliases:"r" group:"Repository"`
	Upstack upstackCmd `cmd:"" aliases:"us" group:"Stack"`
	Branch  branchCmd  `cmd:"" aliases:"b" group:"Branch"`
	Commit  commitCmd  `cmd:"" aliases:"c" group:"Commit"`

	// Navigation
	Up     upCmd     `cmd:"" aliases:"gu" group:"Navigation" help:"Move up one branch"`
	Down   downCmd   `cmd:"" aliases:"gd" group:"Navigation" help:"Move down one branch"`
	Top    topCmd    `cmd:"" aliases:"gt" group:"Navigation" help:"Move to the top of the stack"`
	Bottom bottomCmd `cmd:"" aliases:"gb" group:"Navigation" help:"Move to the bottom of the stack"`
	Trunk  trunkCmd  `cmd:"" aliases:"gg" group:"Navigation" help:"Move to the trunk branch"`
}

func (cmd *mainCmd) AfterApply(kctx *kong.Context, logger *log.Logger) error {
	if cmd.Verbose {
		logger.SetLevel(log.DebugLevel)
	}

	var tokenSource oauth2.TokenSource = &gh.CLITokenSource{}
	if token := cmd.GitHubToken; token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}

	kctx.BindTo(tokenSource, (*oauth2.TokenSource)(nil))
	return nil
}
