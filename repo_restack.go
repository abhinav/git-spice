package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestackCmd struct {
	Worktree bool `short:"w" long:"worktree" help:"Only restack branches in the current worktree."`
}

func (*repoRestackCmd) Help() string {
	return text.Dedent(`
		All tracked branches in the repository are rebased on top of their
		respective bases in dependency order, ensuring a linear history.
	`)
}

func (cmd *repoRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	handler RestackHandler,
	autostashHandler AutostashHandler,
) (retErr error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	cleanup, err := autostashHandler.BeginAutostash(ctx, &autostash.Options{
		Message:   "git-spice: autostash before restacking",
		ResetMode: autostash.ResetHard,
		Branch:    currentBranch,
	})
	if err != nil {
		return err
	}
	defer cleanup(&retErr, nil)

	req := restack.Request{
		Branch:          store.Trunk(),
		Scope:           restack.ScopeUpstackExclusive,
		ContinueCommand: []string{"repo", "restack"},
		SkipCheckout:    true, // caller handles checkout
	}
	// WholeRepo seeds the restack set with stacks rooted at anchors
	// (per-worktree trunks), which are disconnected from the canonical
	// trunk and so are never reached by the upstack-from-trunk walk.
	// It is always needed: a '-w' restack run from inside an anchor
	// worktree must include that worktree's own anchor-rooted stack
	// before WorktreeFilter narrows the set to this worktree.
	req.WholeRepo = true
	if cmd.Worktree {
		// Scope to stacks touching this worktree.
		req.WorktreeFilter = wt.RootDir()
	}

	count, err := handler.Restack(ctx, &req)
	if err != nil {
		return err
	}

	if count == 0 {
		log.Infof("Nothing to restack: no tracked branches available")
		return nil
	}

	if err := wt.CheckoutBranch(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout %v: %w", currentBranch, err)
	}

	log.Infof("Restacked %d branches", count)
	return nil
}
