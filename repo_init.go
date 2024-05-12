package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
	"go.abhg.dev/gs/internal/text"
)

type repoInitCmd struct {
	Trunk  string `placeholder:"BRANCH" help:"Name of the trunk branch"`
	Remote string `placeholder:"NAME" help:"Name of the remote to push changes to"`

	Reset bool `help:"Reset the store if it's already initialized"`
}

func (*repoInitCmd) Help() string {
	return text.Dedent(`
		Sets up a repository for use with gs.
		This isn't strictly necessary to run as most commands will
		auto-initialize the repository as needed.

		Use the --trunk flag to specify the trunk branch.
		This is typically 'main' or 'master',
		and picking one is required for gs to function.

		Use the --remote flag to specify the remote to push changes to.
		If a remote is not specified,
		gs can still be used to stack branches locally.
		However, any commands that require a remote will fail.
	`)
}

func (cmd *repoInitCmd) Run(ctx context.Context, log *log.Logger, globalOpts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	guesser := gs.Guesser{
		Select: func(op gs.GuessOp, opts []string, selected string) (string, error) {
			if !globalOpts.Prompt {
				return "", errNoPrompt
			}

			var msg, desc string
			switch op {
			case gs.GuessRemote:
				msg = "Please select a remote"
				desc = "Merged changes will be pushed to this remote"
			case gs.GuessTrunk:
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
			err := prompt.Run()
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

// ensureStore will open the gs data store in the provided Git repository,
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
		log.Info("Repository not initialized for use with gs. Initializing.")
		if err := (&repoInitCmd{}).Run(ctx, log, opts); err != nil {
			return nil, fmt.Errorf("auto-initialize: %w", err)
		}

		// Assume initialization was a success.
		return state.OpenStore(ctx, repo, log)
	}

	return nil, fmt.Errorf("open store: %w", err)
}
