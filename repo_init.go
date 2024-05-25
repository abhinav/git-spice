package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoInitCmd struct {
	Trunk  string `placeholder:"BRANCH" predictor:"branches" help:"Name of the trunk branch"`
	Remote string `placeholder:"NAME" predictor:"remotes" help:"Name of the remote to push changes to"`

	Reset bool `help:"Reset the store if it's already initialized"`
}

func (*repoInitCmd) Help() string {
	return text.Dedent(`
		Sets up a repository for use.
		This isn't strictly necessary to run as most commands will
		auto-initialize the repository as needed.

		Use the --trunk flag to specify the trunk branch.
		This is typically 'main' or 'master',
		and picking one is required.

		Use the --remote flag to specify the remote to push changes to.
		A remote is not required--local stacking will work without it,
		but any commands that require a remote will fail.
		To add a remote later, re-run this command.
	`)
}

func (cmd *repoInitCmd) Run(ctx context.Context, log *log.Logger, globalOpts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	guesser := spice.Guesser{
		Select: func(op spice.GuessOp, opts []string, selected string) (string, error) {
			if !globalOpts.Prompt {
				return "", errNoPrompt
			}

			var msg, desc string
			switch op {
			case spice.GuessRemote:
				msg = "Please select a remote"
				desc = "Merged changes will be pushed to this remote"
			case spice.GuessTrunk:
				msg = "Please select the trunk branch"
				desc = "Changes will be merged into this branch"
			default:
				must.Failf("unknown guess operation: %v", op)
			}

			options := make([]huh.Option[string], len(opts))
			for i, opt := range opts {
				options[i] = huh.NewOption(opt, opt).
					Selected(opt == selected)
			}

			var result string
			prompt := huh.NewSelect[string]().
				Title(msg).
				Description(desc).
				Options(options...).
				Value(&result)

			err := huh.NewForm(huh.NewGroup(prompt)).
				WithOutput(os.Stdout).
				WithShowHelp(false).
				Run()
			return result, err
		},
	}

	if cmd.Remote == "" {
		cmd.Remote, err = guesser.GuessRemote(ctx, repo)
		if err != nil {
			return fmt.Errorf("guess remote: %w", err)
		}
		if cmd.Remote == "" {
			log.Warn("No remotes found. Commands that require a remote will fail.")
		} else {
			log.Infof("Using remote: %v", cmd.Remote)
		}
	}

	if cmd.Trunk == "" {
		cmd.Trunk, err = guesser.GuessTrunk(ctx, repo, cmd.Remote)
		if err != nil {
			return fmt.Errorf("guess trunk: %w", err)
		}
	}
	must.NotBeBlankf(cmd.Trunk, "trunk branch must have been set")

	_, err = state.InitStore(ctx, state.InitStoreRequest{
		Repository: repo,
		Trunk:      cmd.Trunk,
		Remote:     cmd.Remote,
		Reset:      cmd.Reset,
	})
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	log.Info("Initialized repository", "trunk", cmd.Trunk)
	return nil
}

// ensureStore will open the spice data store in the provided Git repository,
// initializing it with `gs repo init` if it hasn't already been initialized.
//
// This allows nearly any other command to work without initialization
// by auto-initializing the repository at that time.
func ensureStore(
	ctx context.Context,
	repo state.GitRepository,
	log *log.Logger,
	opts *globalOptions,
) (*state.Store, error) {
	store, err := state.OpenStore(ctx, repo, log)
	if err == nil {
		return store, nil
	}

	if errors.Is(err, state.ErrUninitialized) {
		log.Info("Repository not initialized. Initializing.")
		if err := (&repoInitCmd{}).Run(ctx, log, opts); err != nil {
			return nil, fmt.Errorf("auto-initialize: %w", err)
		}

		// Assume initialization was a success.
		return state.OpenStore(ctx, repo, log)
	}

	return nil, fmt.Errorf("open store: %w", err)
}

func ensureRemote(
	ctx context.Context,
	repo spice.GitRepository,
	store *state.Store,
	log *log.Logger,
	globals *globalOptions,
) (string, error) {
	remote, err := store.Remote()
	if err == nil {
		return remote, nil
	}

	if !errors.Is(err, state.ErrNotExist) {
		return "", fmt.Errorf("get remote: %w", err)
	}

	// No remote was specified at init time.
	// Guess or prompt for one and update the store.
	log.Warn("No remote was specified at init time")
	remote, err = (&spice.Guesser{
		Select: func(_ spice.GuessOp, opts []string, selected string) (string, error) {
			if !globals.Prompt {
				return "", errNoPrompt
			}

			options := make([]huh.Option[string], len(opts))
			for i, opt := range opts {
				options[i] = huh.NewOption(opt, opt).
					Selected(opt == selected)
			}

			var result string
			prompt := huh.NewSelect[string]().
				Title("Please select the remote to which you'd like to push your changes").
				Options(options...).
				Value(&result)
			err := huh.NewForm(huh.NewGroup(prompt)).
				WithOutput(os.Stdout).
				WithShowHelp(false).
				Run()
			return result, err
		},
	}).GuessRemote(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("guess remote: %w", err)
	}

	if err := store.SetRemote(ctx, remote); err != nil {
		return "", fmt.Errorf("set remote: %w", err)
	}

	log.Infof("Changed repository remote to %s", remote)
	return remote, nil
}
