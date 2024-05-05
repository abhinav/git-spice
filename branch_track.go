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

func (cmd *branchTrackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := gs.NewService(repo, store, log)

	if cmd.Name == store.Trunk() {
		return fmt.Errorf("cannot track trunk branch")
	}

	if cmd.Base == "" {
		// Find all revisions between the current branch and the trunk branch
		// and check if we know any branches at those revisions.
		// If not, we'll use the trunk branch as the base.
		revs, err := repo.ListCommits(ctx, cmd.Name, store.Trunk())
		if err != nil {
			return fmt.Errorf("list commits: %w", err)
		}

		trackedBranches, err := store.List(ctx)
		if err != nil {
			return fmt.Errorf("list tracked branches: %w", err)
		}

		// Branch hashes will be filled in as needed.
		// A branch hash of ZeroHash means the branch doesn't exist.
		branchHashes := make([]git.Hash, len(trackedBranches))
		hashFor := func(branchIdx int) (git.Hash, error) {
			if hash := branchHashes[branchIdx]; hash != "" {
				return hash, nil
			}

			name := trackedBranches[branchIdx]
			hash, err := repo.PeelToCommit(ctx, name)
			if err != nil {
				if !errors.Is(err, git.ErrNotExist) {
					return "", fmt.Errorf("resolve branch %q: %w", name, err)
				}
				hash = git.ZeroHash
			}
			branchHashes[branchIdx] = hash
			return hash, nil
		}

	revLoop:
		for _, rev := range revs {
			for branchIdx, branchName := range trackedBranches {
				hash, err := hashFor(branchIdx)
				if err != nil {
					return err
				}

				if hash == rev {
					cmd.Base = branchName
					break revLoop
				}
			}
		}

		if cmd.Base == "" {
			cmd.Base = store.Trunk()
		}

		log.Debugf("Detected base branch: %v", cmd.Base)
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
