// gs (git-spice) is a command line tool for stacking Git branches.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"
	"go.abhg.dev/gs/internal/browser"
	"go.abhg.dev/gs/internal/cli/experiment"
	"go.abhg.dev/gs/internal/cli/shorthand"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/handler/cherrypick"
	"go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/split"
	"go.abhg.dev/gs/internal/handler/squash"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/handler/track"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/sigstack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/xec"
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

	// Forges to registry into main at startup besides the defaults.
	_extraForges []forge.Forge
)

var errNoPrompt = ui.ErrPrompt

var _highlightStyle = ui.NewStyle().Foreground(ui.Cyan).Bold(true)

func main() {
	logger := silog.New(os.Stderr, &silog.Options{
		Level: silog.LevelInfo,
	})

	// Register supported forges.
	var forges forge.Registry
	forges.Register(&github.Forge{Log: logger})
	forges.Register(&gitlab.Forge{Log: logger})
	for _, f := range _extraForges {
		forges.Register(f)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sigStack sigstack.Stack
	sigc := make(chan sigstack.Signal, 1)
	sigStack.Notify(sigc, os.Interrupt)
	go func() {
		select {
		case <-sigc:
			sigStack.Stop(sigc)
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
	for f := range forges.All() {
		if plugin := f.CLIPlugin(); plugin != nil {
			cmd.Plugins = append(cmd.Plugins, plugin)
		}
	}

	cmdName := filepath.Base(os.Args[0])
	parser, err := kong.New(&cmd,
		kong.Name(cmdName),
		kong.Description("gs (git-spice) is a command line tool for stacking Git branches."),
		kong.Resolvers(spiceConfig),
		kong.Bind(logger, &forges, &sigStack),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindTo(spiceConfig, (*experiment.Enabler)(nil)),
		kong.BindTo(secretStash, (*secret.Stash)(nil)),
		kong.Vars{
			// Default to prompting only when the terminal is interactive.
			"defaultPrompt": strconv.FormatBool(isatty.IsTerminal(os.Stdin.Fd())),
		},
		kong.UsageOnError(),
		kong.Help(helpPrinter),
	)
	if err != nil {
		panic(err)
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
		komplete.WithPredictor("forges", komplete.PredictFunc(predictForges(&forges))),
	)

	args := os.Args[1:]
	if len(args) == 0 {
		// If invoked with no arguments, show help,
		// but then exit with a non-zero status code.
		args = []string{"--help"}
		parser.Exit = func(int) {
			fmt.Fprintln(os.Stderr)
			logger.Fatalf("%v: please provide a command", cmdName)
		}
	} else if shellCmd, ok := spiceConfig.ShellCommand(args[0]); ok {
		const (
			recursionDepthLimit  = 10
			recursionDepthEnvVar = "__GS_SHELL_COMMAND_DEPTH"
		)

		var depth int
		if depthStr := os.Getenv(recursionDepthEnvVar); depthStr != "" {
			// Prevent infinite loops by limiting recursion depth.
			var err error
			depth, err = strconv.Atoi(depthStr)
			if err != nil {
				// Assume depth is 0 if invalid.
				depth = 0
			}
		}
		if depth >= recursionDepthLimit {
			logger.Fatalf("%v: shell command recursion depth limit exceeded (%d): %v",
				cmdName, recursionDepthLimit, args[0])
		}

		// The first argument might be a shell command alias.
		// If so, invoke the shell command directly and exit.
		shArgs := []string{"-c", shellCmd}
		if len(args) > 1 {
			shArgs = append(shArgs, cmdName) // $0
			shArgs = append(shArgs, args[1:]...)
		}

		if err := xec.Command(ctx, logger, "sh", shArgs...).
			WithStdout(os.Stdout).
			WithStderr(os.Stderr).
			AppendEnv(fmt.Sprintf("%v=%d", recursionDepthEnvVar, depth+1)).
			Run(); err != nil {
			logger.Fatalf("%v: %v", cmdName, err)
		}
		return
	} else {
		// Otherwise, expand the shorthand,
		// parse the arguments, and proceed as usual.
		args = shorthand.Expand(shorthands, args)
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		logger.Fatalf("%v: %v", cmdName, err)
	}

	if err := cmd.Profile.Start(); err != nil {
		logger.Error("Error creating trace file", "error", err)
	}

	if err := kctx.Run(builtinShorthands); err != nil {
		logger.Fatalf("%v: %v", cmdName, err)
	}

	if err := cmd.Profile.Stop(); err != nil {
		logger.Error("Error closing trace file", "error", err)
	}
}

type mainCmd struct {
	kong.Plugins
	experiment.Check

	Profile ProfileFlags `embed:""`

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

	Version versionCmd `cmd:"" help:"Print version information and quit"`

	Internal internalCmd `cmd:"" hidden:"" help:"For internal use only."`

	// Hidden commands:
	DumpMD dumpMarkdownCmd `name:"dumpmd" hidden:"" cmd:"" help:"Dump a Markdown reference to stdout and quit"`
}

func (cmd *mainCmd) AfterApply(ctx context.Context, kctx *kong.Context, logger *silog.Logger) error {
	if cmd.Globals.Verbose {
		logger.SetLevel(silog.LevelDebug)
	}

	view, err := _buildView(os.Stdin, kctx.Stderr, cmd.Globals.Prompt)
	if err != nil {
		return fmt.Errorf("build view: %w", err)
	}
	kctx.BindTo(view, (*ui.View)(nil))

	// TODO: bind interfaces, not values
	// TODO:
	// introduce a type that defaults to the current branch
	// so that commands that don't need worktree aren't forced to use it
	// just to get the current branch name.

	return errors.Join(
		kctx.BindSingletonProvider(func() (*git.Worktree, error) {
			return git.OpenWorktree(ctx, ".", git.OpenOptions{
				Log: logger,
			})
		}),
		kctx.BindSingletonProvider(func(wt *git.Worktree) (*git.Repository, error) {
			return wt.Repository(), nil
		}),
		kctx.BindSingletonProvider(func(repo *git.Repository, wt *git.Worktree) (*state.Store, error) {
			return ensureStore(ctx, repo, wt, logger, view)
		}),
		kctx.BindSingletonProvider(func(
			repo *git.Repository,
			wt *git.Worktree,
			store *state.Store,
			forges *forge.Registry,
		) (*spice.Service, error) {
			return spice.NewService(repo, wt, store, forges, logger), nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			wt *git.Worktree,
			svc *spice.Service,
		) (AutostashHandler, error) {
			return &autostash.Handler{
				Log:      log,
				Worktree: wt,
				Service:  svc,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			view ui.View,
			repo *git.Repository,
			store *state.Store,
			svc *spice.Service,
		) (TrackHandler, error) {
			return &track.Handler{
				Log:        log,
				View:       view,
				Repository: repo,
				Store:      store,
				Service:    svc,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			store *state.Store,
			repo *git.Repository,
			wt *git.Worktree,
			svc *spice.Service,
			trackHandler TrackHandler,
		) (CheckoutHandler, error) {
			return &checkout.Handler{
				Stdout:     kctx.Stdout,
				Log:        log,
				Store:      store,
				Repository: repo,
				Worktree:   wt,
				Track:      trackHandler,
				Service:    svc,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			store *state.Store,
			wt *git.Worktree,
			svc *spice.Service,
			secretStash secret.Stash,
			forges *forge.Registry,
		) (SubmitHandler, error) {
			return &submit.Handler{
				Log:        log,
				View:       view,
				Repository: wt.Repository(),
				Worktree:   wt,
				Store:      store,
				Service:    svc,
				Browser:    _browserLauncher,
				FindRemote: func(ctx context.Context) (string, error) {
					return ensureRemote(ctx, wt.Repository(), store, log, view)
				},
				OpenRemoteRepository: func(ctx context.Context, remote string) (forge.Repository, error) {
					return openRemoteRepository(ctx, log, secretStash, forges, wt.Repository(), remote)
				},
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			worktree *git.Worktree,
			store *state.Store,
			svc *spice.Service,
		) (RestackHandler, error) {
			return &restack.Handler{
				Log:      log,
				Worktree: worktree,
				Store:    store,
				Service:  svc,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			store *state.Store,
			wt *git.Worktree,
			svc *spice.Service,
		) (DeleteHandler, error) {
			return &delete.Handler{
				Log:        log,
				View:       view,
				Repository: wt.Repository(),
				Worktree:   wt,
				Store:      store,
				Service:    svc,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			repo *git.Repository,
			wt *git.Worktree,
			store *state.Store,
			svc *spice.Service,
			restackHandler RestackHandler,
		) (SquashHandler, error) {
			return &squash.Handler{
				Log:        log,
				Repository: repo,
				Worktree:   wt,
				Store:      store,
				Service:    svc,
				Restack:    restackHandler,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			view ui.View,
			repo *git.Repository,
			store *state.Store,
			svc *spice.Service,
			forges *forge.Registry,
		) (SplitHandler, error) {
			return &split.Handler{
				Log:            log,
				View:           view,
				Repository:     repo,
				Store:          store,
				Service:        svc,
				FindForge:      forges.Lookup,
				HighlightStyle: _highlightStyle,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			repo *git.Repository,
			wt *git.Worktree,
			restackHandler RestackHandler,
			autostashHandler AutostashHandler,
		) (CherryPickHandler, error) {
			return &cherrypick.Handler{
				Log:        log,
				Repository: repo,
				Worktree:   wt,
				Restack:    restackHandler,
				Autostash:  autostashHandler,
			}, nil
		}),
		kctx.BindSingletonProvider(func(
			log *silog.Logger,
			view ui.View,
			repo *git.Repository,
			wt *git.Worktree,
			store *state.Store,
			svc *spice.Service,
			secretStash secret.Stash,
			forges *forge.Registry,
			deleteHandler DeleteHandler,
			restackHandler RestackHandler,
		) (SyncHandler, error) {
			remote, err := ensureRemote(ctx, repo, store, log, view)
			// TODO: move ensure remote to Service
			if err != nil {
				return nil, err
			}

			remoteRepo, err := openRemoteRepositorySilent(ctx, secretStash, forges, repo, remote)
			if err != nil {
				var unsupported *unsupportedForgeError
				if !errors.As(err, &unsupported) {
					return nil, err
				}
				remoteRepo = nil
			}

			return &sync.Handler{
				Log:              log,
				View:             view,
				Repository:       repo,
				Worktree:         wt,
				Store:            store,
				Service:          svc,
				Delete:           deleteHandler,
				Restack:          restackHandler,
				Remote:           remote,
				RemoteRepository: remoteRepo,
			}, nil
		}),
	)
}

type AutostashHandler interface {
	BeginAutostash(ctx context.Context, opts *autostash.Options) (func(*error), error)
	RestoreAutostash(ctx context.Context, stashHash string) error
}

var _ AutostashHandler = (*autostash.Handler)(nil)

var _buildView = func(stdin io.Reader, stderr io.Writer, interactive bool) (ui.View, error) {
	if interactive {
		return &ui.TerminalView{
			R: stdin,
			W: stderr,
		}, nil
	}
	return &ui.FileView{W: stderr}, nil
}
