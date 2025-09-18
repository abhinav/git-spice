package main

import (
	"context"
	"errors"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

type internalCmd struct {
	AutostashPop internalAutostashPop `cmd:""`
}

type internalAutostashPop struct {
	Hash string `name:"hash" arg:"" required:""`
}

func (cmd *internalAutostashPop) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
) error {
	err := wt.StashApply(ctx, cmd.Hash)
	if err == nil {
		log.Info("Applied autostash")
		return nil
	}

	// If autostash apply fails,
	// log the error, and save the stash for restoration.
	log.Error("Failed to apply autostashed changes", "error", err)
	if err := wt.StashStore(ctx, git.Hash(cmd.Hash), "git-spice: autostash failed to apply"); err != nil {
		// If even stash store fails, there's nothing we can do.
		// Tell the user to manually recover the stash.
		log.Error("Failed to save autostashed changes", "error", err)
		log.Errorf("You can try recovering them with 'git stash apply %s'", cmd.Hash)
		return errors.New("stashed changes could not be applied or saved")
	}

	log.Error("Your changes are safe in the stash. You can:")
	log.Error("- apply them with 'git stash pop';")
	log.Error("- or drop them with 'git stash drop'")
	return errors.New("autostashed changes could not be applied")
}
