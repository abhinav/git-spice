package track

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// BranchRequest is the request for tracking a branch.
type BranchRequest struct {
	// Branch is the name of the branch to track.
	Branch string // required

	// Base is the name of the base branch this branch merges into.
	// If not provided, it will be guessed based on other tracked branches.
	Base string // optional
}

// TrackBranch tracks a branch defined in the Git repository.
func (h *Handler) TrackBranch(ctx context.Context, req *BranchRequest) error {
	must.NotBeBlankf(req.Branch, "branch name must not be blank")

	log, store := h.Log, h.Store
	if req.Branch == store.Trunk() {
		return errors.New("cannot track trunk branch")
	}

	if req.Base == "" {
		log.Debugf("%v: looking for base branch", req.Branch)

		base, err := guessBaseBranch(ctx, store, h.Repository, req.Branch)
		if err != nil {
			log.Warn("could not guess base branch, using trunk", "error", err)
			base = store.Trunk()
		}

		req.Base = base
		log.Infof("%v: using base branch: %v", req.Branch, req.Base)
	}

	baseHash, err := h.Repository.PeelToCommit(ctx, req.Base)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	msg := fmt.Sprintf("track %v with base %v", req.Branch, req.Base)
	tx := store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:     req.Branch,
		Base:     req.Base,
		BaseHash: baseHash,
	},
	); err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	if err := tx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	log.Infof("%v: tracking with base %v", req.Branch, req.Base)

	if err := h.Service.VerifyRestacked(ctx, req.Branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		if errors.As(err, &restackErr) {
			log.Infof("%v: branch is behind its base and needs to be restacked.", req.Branch)
			log.Infof("%v: run 'gs branch restack --branch=%v' to restack it", req.Branch, req.Branch)
		} else {
			log.Warnf("%v: stack state verification failed: %v", req.Branch, err)
		}
	}

	return nil
}

func guessBaseBranch(
	ctx context.Context,
	store Store,
	repo GitRepository,
	branch string,
) (string, error) {
	trackedBranches, err := sliceutil.CollectErr(store.ListBranches(ctx))
	if err != nil {
		return "", fmt.Errorf("list tracked branches: %w", err)
	}
	if len(trackedBranches) == 0 {
		// No branches tracked, use trunk as base.
		return store.Trunk(), nil
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

	branchHash, err := repo.PeelToCommit(ctx, branch)
	if err != nil {
		return "", fmt.Errorf("peel to commit: %w", err)
	}

	trunkHash, err := repo.PeelToCommit(ctx, store.Trunk())
	if err != nil {
		return "", fmt.Errorf("peel to commit: %w", err)
	}

	// Find all revisions between the current branch and the trunk branch
	// and check if we know any branches at those revisions.
	// If not, we'll use the trunk branch as the base.
	revIter := repo.ListCommits(ctx,
		git.CommitRangeFrom(branchHash).ExcludeFrom(trunkHash))
	for rev, err := range revIter {
		if err != nil {
			return "", fmt.Errorf("list commits: %w", err)
		}

		for branchIdx, base := range trackedBranches {
			if base == branch {
				continue
			}

			hash, err := hashFor(branchIdx)
			if err != nil {
				return "", err
			}

			if hash == rev {
				return base, nil
			}
		}
	}

	return store.Trunk(), nil
}
