// gs (git-spice) is a command line tool for stacking Git branches.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mattn/go-isatty"
	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/cli/shorthand"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/komplete"
)

// Version of git-spice.
// Set by goreleaser at build time.
var _version = "dev"

var (
	// _secretStash is the secret stash used by the application.
	//
	// This is overridden in tests to use a memory stash.
	_secretStash secret.Stash = new(secret.Keyring)

	// _browserLauncher opens URLs in the user's configured browser.
	_browserLauncher browser.Launcher = new(browser.Browser)
)

var errNoPrompt = fmt.Errorf("not allowed to prompt for input")

var _highlightStyle = ui.NewStyle().Foreground(ui.Cyan).Bold(true)

func main() {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level: log.InfoLevel,
	})

	// Register supported forges.
	forge.Register(&github.Forge{Log: logger})
	forge.Register(&gitlab.Forge{Log: logger})

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

	// On macOS, UserConfigDir always returns ~/Library/Application Support.
	// XDG_CONFIG_HOME should take precedence if set.
	userConfigDir := os.Getenv("XDG_CONFIG_HOME")
	if userConfigDir == "" {
		var err error
		userConfigDir, err = os.UserConfigDir()
		if err != nil {
			logger.Fatalf("Error getting user config directory: %v", err)
		}
	}

	spiceConfig, err := spice.LoadConfig(
		ctx,
		git.NewConfig(git.ConfigOptions{Log: logger}),
		spice.ConfigOptions{
			Log: logger,
		},
	)
	if err != nil {
		logger.Error("Error loading spice configuration; continuing without it.", "error", err)
	}

	secretStash := &secret.FallbackStash{
		Primary: _secretStash,
		Secondary: &secret.InsecureStash{
			Path: filepath.Join(filepath.Join(userConfigDir, "git-spice"), "secrets.json"),
			Log:  logger,
		},
	}

	var cmd mainCmd

	// Forges may register additional command line flags
	// by implementing CLIPlugin.
	for f := range forge.All {
		if plugin := f.CLIPlugin(); plugin != nil {
			cmd.Plugins = append(cmd.Plugins, plugin)
		}
	}

	cmdName := filepath.Base(os.Args[0])
	parser, err := kong.New(&cmd,
		kong.Name(cmdName),
		kong.Description("gs (git-spice) is a command line tool for stacking Git branches."),
		kong.Resolvers(spiceConfig),
		kong.Bind(logger),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindTo(secretStash, (*secret.Stash)(nil)),
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
				_, _ = fmt.Fprintf(ctx.Stdout,
					"\nAliases can be combined to form shorthands for commands. For example:\n"+
						"  %[1]v bc => %[1]v branch create\n"+
						"  %[1]v cc => %[1]v commit create\n",
					cmdName,
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

	builtinShorthands, err := shorthand.NewBuiltin(parser.Model)
	if err != nil {
		panic(err)
	}

	// user-configured shorthands take precedence over builtins.
	shorthands := shorthand.Sources{spiceConfig, builtinShorthands}

	komplete.Run(parser,
		komplete.WithTransformCompleted(func(args []string) []string {
			return shorthand.Expand(shorthands, args)
		}),
		komplete.WithPredictor("branches", komplete.PredictFunc(predictBranches)),
		komplete.WithPredictor("trackedBranches", komplete.PredictFunc(predictTrackedBranches)),
		komplete.WithPredictor("remotes", komplete.PredictFunc(predictRemotes)),
		komplete.WithPredictor("dirs", komplete.PredictFunc(predictDirs)),
		komplete.WithPredictor("forges", komplete.PredictFunc(predictForges)),
	)

	args := os.Args[1:]
	if len(args) == 0 {
		// If invoked with no arguments, show help,
		// but then exit with a non-zero status code.
		args = []string{"--help"}
		parser.Exit = func(int) {
			logger.Print("")
			logger.Fatalf("%v: please provide a command", cmdName)
		}
	} else {
		// Otherwise, expand the shorthands before parsing.
		args = shorthand.Expand(shorthands, args)
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		logger.Fatalf("%v: %v", cmdName, err)
	}

	if err := kctx.Run(builtinShorthands); err != nil {
		logger.Fatalf("%v: %v", cmdName, err)
	}
}

type mainCmd struct {
	kong.Plugins

	// Global options that are never accessed directly by subcommands.
	Globals struct {
		// Flags with built-in side effects.
		Version versionFlag        `help:"Print version information and quit"`
		Verbose bool               `short:"v" help:"Enable verbose output" env:"GIT_SPICE_VERBOSE"`
		Dir     kong.ChangeDirFlag `short:"C" placeholder:"DIR" help:"Change to DIR before doing anything" predictor:"dirs"`
		Prompt  bool               `name:"prompt" negatable:"" default:"${defaultPrompt}" help:"Whether to prompt for missing information"`
	} `embed:"" group:"globals"`

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
	if cmd.Globals.Verbose {
		logger.SetLevel(log.DebugLevel)
	}

	view, err := _buildView(os.Stdin, kctx.Stderr, cmd.Globals.Prompt)
	if err != nil {
		return fmt.Errorf("build view: %w", err)
	}
	kctx.BindTo(view, (*ui.View)(nil))

	return nil
}

var _buildView = func(stdin io.Reader, stderr io.Writer, interactive bool) (ui.View, error) {
	if interactive {
		return &ui.TerminalView{
			R: stdin,
			W: stderr,
		}, nil
	}
	return &ui.FileView{W: stderr}, nil
}
