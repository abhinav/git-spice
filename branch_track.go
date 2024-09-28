package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchTrackCmd struct {
	Base   string `short:"b" placeholder:"BRANCH" help:"Base branch this merges into" predictor:"trackedBranches"`
	Branch string `arg:"" optional:"" help:"Name of the branch to track" predictor:"branches"`
}

func (*branchTrackCmd) Help() string {
	return text.Dedent(`
		A branch must be tracked to be able to run gs operations on it.
		Use 'gs branch create' to automatically track new branches.

		The base is guessed by comparing against other tracked branches.
		Use --base to specify a base explicitly.
	`)
}

func (cmd *branchTrackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Branch == "" {
		cmd.Branch, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	if cmd.Branch == store.Trunk() {
		return fmt.Errorf("cannot track trunk branch")
	}

	if cmd.Base == "" {
		branchHash, err := repo.PeelToCommit(ctx, cmd.Branch)
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}

		trunkHash, err := repo.PeelToCommit(ctx, store.Trunk())
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}

		// Find all revisions between the current branch and the trunk branch
		// and check if we know any branches at those revisions.
		// If not, we'll use the trunk branch as the base.
		revs, err := repo.ListCommits(ctx,
			git.CommitRangeFrom(branchHash).ExcludeFrom(trunkHash))
		if err != nil {
			return fmt.Errorf("list commits: %w", err)
		}

		trackedBranches, err := store.ListBranches(ctx)
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
				if branchName == cmd.Branch {
					continue
				}

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

	msg := fmt.Sprintf("track %v with base %v", cmd.Branch, cmd.Base)
	tx := store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:     cmd.Branch,
		Base:     cmd.Base,
		BaseHash: baseHash,
	},
	); err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	if err := tx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	log.Infof("%v: tracking with base %v", cmd.Branch, cmd.Base)

	if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if errors.As(err, &restackErr) {
			log.Warnf("%v: needs to be restacked: run 'gs branch restack --branch=%v'", cmd.Branch, cmd.Branch)
		}
		log.Warnf("error checking branch: %v", err)
	}

	return nil
}
