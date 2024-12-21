package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type repoInitCmd struct {
	Trunk  string `placeholder:"BRANCH" predictor:"branches" help:"Name of the trunk branch"`
	Remote string `placeholder:"NAME" predictor:"remotes" help:"Name of the remote to push changes to"`

	Reset bool `help:"Forget all information about the repository"`
}

func (*repoInitCmd) Help() string {
	return text.Dedent(`
		A trunk branch is required.
		This is the branch that changes will be merged into.
		A prompt will ask for one if not provided with --trunk.

		Most branch stacking operations are local
		and do not require a network connection.
		For operations that push or pull commits, a remote is required.
		A prompt will ask for one during initialization
		if not provided with --remote.

		Re-run the command on an already initialized repository
		to change the trunk or remote.
		If the trunk branch is changed on re-initialization,
		existing branches stacked on the old trunk
		will be updated to point to the new trunk.

		Re-run with --reset to discard all stored information
		and untrack all branches.
	`)
}

func (cmd *repoInitCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
) error {
	guesser := spice.Guesser{
		Select: func(op spice.GuessOp, opts []string, selected string) (string, error) {
			if !ui.Interactive(view) {
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

			var result string
			prompt := ui.NewSelect[string]().
				WithValue(&result).
				With(ui.ComparableOptions(selected, opts...)).
				WithTitle(msg).
				WithDescription(desc)
			if err := ui.Run(view, prompt); err != nil {
				return "", err
			}

			return result, nil
		},
	}

	if cmd.Remote == "" {
		var err error
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
		var err error
		cmd.Trunk, err = guesser.GuessTrunk(ctx, repo, cmd.Remote)
		if err != nil {
			return fmt.Errorf("guess trunk: %w", err)
		}
	}
	must.NotBeBlankf(cmd.Trunk, "trunk branch must have been set")

	_, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:     newRepoStorage(repo, log),
		Trunk:  cmd.Trunk,
		Remote: cmd.Remote,
		Reset:  cmd.Reset,
	})
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	log.Info("Initialized repository", "trunk", cmd.Trunk)
	return nil
}

const (
	_dataRef     = "refs/spice/data"
	_authorName  = "git-spice"
	_authorEmail = "git-spice@localhost"
)

func newRepoStorage(repo storage.GitRepository, log *log.Logger) *storage.DB {
	return storage.NewDB(storage.NewGitBackend(storage.GitConfig{
		Repo:        repo,
		Ref:         _dataRef,
		AuthorName:  _authorName,
		AuthorEmail: _authorEmail,
		Log:         log,
	}))
}

// ensureStore will open the spice data store in the provided Git repository,
// initializing it with `gs repo init` if it hasn't already been initialized.
//
// This allows nearly any other command to work without initialization
// by auto-initializing the repository at that time.
func ensureStore(
	ctx context.Context,
	repo *git.Repository,
	log *log.Logger,
	view ui.View,
) (*state.Store, error) {
	db := newRepoStorage(repo, log)
	store, err := state.OpenStore(ctx, db, log)
	if err == nil {
		return store, nil
	}

	if errors.Is(err, state.ErrUninitialized) {
		log.Info("Repository not initialized. Initializing.")
		if err := (&repoInitCmd{}).Run(ctx, log, view, repo); err != nil {
			return nil, fmt.Errorf("auto-initialize: %w", err)
		}

		// Assume initialization was a success.
		return state.OpenStore(ctx, db, log)
	}

	return nil, fmt.Errorf("open store: %w", err)
}

func ensureRemote(
	ctx context.Context,
	repo spice.GitRepository,
	store *state.Store,
	log *log.Logger,
	view ui.View,
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
			if !ui.Interactive(view) {
				return "", errNoPrompt
			}

			result := selected
			prompt := ui.NewSelect[string]().
				WithValue(&result).
				With(ui.ComparableOptions(selected, opts...)).
				WithTitle("Please select a remote").
				WithDescription("Changes will be pushed to this remote")
			if err := ui.Run(view, prompt); err != nil {
				return "", err
			}

			return result, nil
		},
	}).GuessRemote(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("guess remote: %w", err)
	}

	if err := store.SetRemote(ctx, remote); err != nil {
		return "", fmt.Errorf("set remote: %w", err)
	}

	// TODO: this should also update the Forge associated with the spice.Service.

	log.Infof("Changed repository remote to %s", remote)
	return remote, nil
}
