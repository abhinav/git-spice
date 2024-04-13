package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/state"
)

type branchTrackCmd struct {
	Base string `short:"b" help:"Base branch this merges into"`
	Name string `arg:"" optional:"" help:"Name of the branch to track"`
}

func (cmd *branchTrackCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Name == "" {
		cmd.Name, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	svc := gs.NewService(repo, store, log)

	if cmd.Name == store.Trunk() {
		return fmt.Errorf("cannot track trunk branch")
	}

	// TODO: handle already tracking
	// TODO: auto-detect base branch with revision matching

	if cmd.Base == "" {
		return fmt.Errorf("missing required flag -p")
	}

	baseHash, err := repo.PeelToCommit(ctx, cmd.Base)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	err = store.Update(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     cmd.Name,
				Base:     cmd.Base,
				BaseHash: baseHash,
			},
		},
		Message: fmt.Sprintf("track %v with base %v", cmd.Name, cmd.Base),
	})
	if err != nil {
		return fmt.Errorf("set branch: %w", err)
	}

	log.Infof("%v: tracking with base %v", cmd.Name, cmd.Base)

	if err := svc.VerifyRestacked(ctx, cmd.Name); err != nil {
		if errors.Is(err, gs.ErrNeedsRestack) {
			log.Warnf("%v: needs to be restacked: run 'gs branch restack %v'", cmd.Name, cmd.Name)
		}
		log.Warnf("error checking branch: %v", err)
	}

	return nil
}
