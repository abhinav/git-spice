package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

const _forkModeFooter = "Using a different push remote will operate git-spice in Fork mode:\n" +
	"Local operations will operate normally, but remote operations will be affected.\n" +
	"In particular, submit will create CRs only for trunk-based branches,\n" +
	"while still pushing all branches to the push remote."

type repoInitCmd struct {
	Trunk    string `placeholder:"BRANCH" predictor:"branches" help:"Name of the trunk branch"`
	Remote   string `placeholder:"NAME" predictor:"remotes" help:"Name of the remote to push submitted branches to"`
	Upstream string `placeholder:"NAME" predictor:"remotes" help:"Name of the remote to open change requests against"`

	Reset bool `help:"Forget all information about the repository"`
}

func (*repoInitCmd) Help() string {
	return text.Dedent(`
		A trunk branch is required.
		This is the branch that changes will be merged into.
		A prompt will ask for one if not provided with --trunk.

		Most branch stacking operations are local
		and do not require a network connection.
		For operations that push or pull commits, remotes are required.
		A prompt will ask for them during initialization
		if not provided with --remote.

		The upstream remote hosts trunk and receives change requests.
		The push remote receives submitted branch pushes.
		If only --remote is provided,
		it is used as both the upstream and push remote.

		Re-run the command on an already initialized repository
		to change the trunk or remotes.
		If the trunk branch is changed on re-initialization,
		existing branches stacked on the old trunk
		will be updated to point to the new trunk.

		Re-run with --reset to discard all stored information
		and untrack all branches.
	`)
}

func (cmd *repoInitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
) error {
	guesser := newRepoGuesser(view)

	remote, err := cmd.resolveRemote(ctx, repo, &guesser)
	if err != nil {
		return err
	}

	logUsingRemote(log, remote)

	if cmd.Trunk == "" {
		var err error
		cmd.Trunk, err = guesser.GuessTrunk(ctx, repo, wt, cmd.Upstream)
		if err != nil {
			return fmt.Errorf("guess trunk: %w", err)
		}
	} else if !repo.BranchExists(ctx, cmd.Trunk) {
		// User-provided trunk must be a local branch.
		log.Errorf("Are you sure %v is a local branch?", cmd.Trunk)
		return fmt.Errorf("not a branch: %v", cmd.Trunk)
	}
	must.NotBeBlankf(cmd.Trunk, "trunk branch must have been set")

	_, err = state.InitStore(ctx, state.InitStoreRequest{
		DB:     newRepoStorage(repo, log),
		Trunk:  cmd.Trunk,
		Remote: remote,
		Reset:  cmd.Reset,
	})
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	// If trunk is behind upstream, warn the user.
	trunkHash, err1 := repo.PeelToCommit(ctx, cmd.Trunk)
	upstreamHash, err2 := repo.PeelToCommit(ctx, cmd.Upstream+"/"+cmd.Trunk)
	if err := errors.Join(err1, err2); err == nil {
		count, err := repo.CountCommits(ctx,
			git.CommitRangeFrom(upstreamHash).ExcludeFrom(trunkHash))
		if err == nil && count > 0 {
			log.Warnf("%v is behind upstream by %d commits", cmd.Trunk, count)
			log.Warnf("Please run '%s repo sync' before other git-spice commands.", cli.Name())
		}
	}

	log.Info("Initialized repository", "trunk", cmd.Trunk)
	return nil
}

// repoInitRemoteGuesser guesses remotes for repository initialization.
type repoInitRemoteGuesser interface {
	GuessUpstreamRemote(context.Context, spice.GitRepository) (string, error)
	GuessPushRemote(context.Context, spice.GitRepository, string) (string, error)
}

func (cmd *repoInitCmd) resolveRemote(
	ctx context.Context,
	repo spice.GitRepository,
	guesser repoInitRemoteGuesser,
) (state.Remote, error) {
	// If only one of the flags is set,
	// assume they're both the same remote.
	upstream := cmp.Or(cmd.Upstream, cmd.Remote)
	push := cmp.Or(cmd.Remote, upstream)

	// If no remotes were specified on the CLI,
	// guess or prompt for upstream first.
	if upstream == "" {
		var err error
		upstream, err = guesser.GuessUpstreamRemote(ctx, repo)
		if err != nil {
			return state.Remote{}, fmt.Errorf("guess upstream remote: %w", err)
		}
	}
	// Push remote next, defaulting to upstream.
	if push == "" {
		var err error
		push, err = guesser.GuessPushRemote(ctx, repo, upstream)
		if err != nil {
			return state.Remote{}, fmt.Errorf("guess push remote: %w", err)
		}
	}

	remote := state.Remote{
		Upstream: upstream,
		Push:     push,
	}
	cmd.Upstream = remote.Upstream
	cmd.Remote = remote.Push
	return remote, nil
}

func newRepoGuesser(view ui.View) spice.Guesser {
	return spice.Guesser{
		Select: func(op spice.GuessOp, opts []string, selected string) (string, error) {
			if !ui.Interactive(view) {
				return "", errNoPrompt
			}

			var msg, desc string
			switch op {
			case spice.GuessPushRemote:
				msg = "Please select a push remote"
				desc = "Submitted branches will be pushed to this remote"
			case spice.GuessUpstreamRemote:
				msg = "Please select an upstream remote"
				desc = "Change requests will be opened against this remote"
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
			if op == spice.GuessPushRemote && selected != "" {
				prompt.WithFooterFunc(func(remote string) string {
					if remote == selected {
						return ""
					}
					return _forkModeFooter
				})
			}
			if err := ui.Run(view, prompt); err != nil {
				return "", err
			}

			return result, nil
		},
	}
}

const (
	_dataRef     = "refs/spice/data"
	_authorName  = "git-spice"
	_authorEmail = "git-spice@localhost"
)

func newRepoStorage(repo *git.Repository, log *silog.Logger) *storage.DB {
	log = cmp.Or(log, silog.Nop())
	return storage.NewDB(storage.NewGitBackend(storage.GitConfig{
		Repo:        repo.WithLogger(log.Downgrade()),
		Ref:         _dataRef,
		AuthorName:  _authorName,
		AuthorEmail: _authorEmail,
		Log:         log,
	}))
}

// ensureStore will open the spice data store in the provided Git repository,
// initializing it with `git-spice repo init` if it hasn't already been initialized.
//
// This allows nearly any other command to work without initialization
// by auto-initializing the repository at that time.
func ensureStore(
	ctx context.Context,
	repo *git.Repository,
	wt *git.Worktree,
	log *silog.Logger,
	view ui.View,
) (*state.Store, error) {
	db := newRepoStorage(repo, log)
	store, err := state.OpenStore(ctx, db, log)
	if err == nil {
		return store, nil
	}

	if errors.Is(err, state.ErrUninitialized) {
		log.Info("Repository not initialized. Initializing.")
		if err := (&repoInitCmd{}).Run(ctx, log, view, repo, wt); err != nil {
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
	log *silog.Logger,
	view ui.View,
) (state.Remote, error) {
	remote, err := store.Remote()
	if err == nil {
		return remote, nil
	}

	if !errors.Is(err, state.ErrNotExist) {
		return state.Remote{}, fmt.Errorf("get remote: %w", err)
	}

	// No remote was specified at init time.
	// Guess or prompt for remotes and update the store.
	log.Warn("No remote was specified at init time")
	guesser := newRepoGuesser(view)

	upstream, err := guesser.GuessUpstreamRemote(ctx, repo)
	if err != nil {
		return state.Remote{}, fmt.Errorf("guess upstream remote: %w", err)
	}

	remote = state.Remote{
		Upstream: upstream,
	}
	remote.Push, err = guesser.GuessPushRemote(ctx, repo, upstream)
	if err != nil {
		return state.Remote{}, fmt.Errorf("guess push remote: %w", err)
	}

	if err := store.SetRemote(ctx, remote); err != nil {
		return state.Remote{}, fmt.Errorf("set remote: %w", err)
	}

	// TODO: this should also update the Forge associated with the spice.Service.

	logChangedRemote(log, remote)
	return remote, nil
}

func logUsingRemote(log *silog.Logger, remote state.Remote) {
	if remote == (state.Remote{}) {
		log.Warn("No remotes found. Commands that require a remote will fail.")
		return
	}
	if remote.ForkMode() {
		log.Infof("Using upstream remote: %s", remote.Upstream)
		log.Infof("Using push remote: %s", remote.Push)
		return
	}
	log.Infof("Using remote: %s", cmp.Or(remote.Upstream, remote.Push))
}

func logChangedRemote(log *silog.Logger, remote state.Remote) {
	if remote == (state.Remote{}) {
		return
	}
	if remote.ForkMode() {
		log.Infof("Changed repository upstream remote to %s", remote.Upstream)
		log.Infof("Changed repository push remote to %s", remote.Push)
		return
	}
	log.Infof("Changed repository remote to %s", cmp.Or(remote.Upstream, remote.Push))
}
