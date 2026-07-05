package spice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// ForgetBranch stops tracking a branch,
// updating the upstacks for it to point to its base.
func (s *Service) ForgetBranch(ctx context.Context, name string) error {
	// This does not use LookupBranch because we don't care if the branch
	// doesn't actually exist, we just want to update the upstacks.
	branch, err := s.store.LookupBranch(ctx, name)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("lookup branch: %w", err)
	}

	// Similarly, this doesn't use ListAbove
	// because we don't want the deleted branch to be removed yet.
	branchTx := s.store.BeginBranchTx()
	for candidate, err := range s.store.ListBranches(ctx) {
		if err != nil {
			return fmt.Errorf("list branches: %w", err)
		}

		if candidate == name {
			continue
		}

		info, err := s.store.LookupBranch(ctx, candidate)
		if err != nil {
			return fmt.Errorf("lookup %v: %w", candidate, err)
		}

		if info.Base != name {
			continue
		}

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:     candidate,
			Base:     branch.Base,
			BaseHash: branch.BaseHash,
		}); err != nil {
			return fmt.Errorf("change base of %v to %v: %w", candidate, branch.Base, err)
		}
		s.log.Debug("Updating upstack branch to new base",
			"branch", candidate,
			"newBase", branch.Base,
		)

	}

	if err := branchTx.Delete(ctx, name); err != nil {
		return fmt.Errorf("delete branch %v: %w", name, err)
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("untrack branch %q", name)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}
	s.log.Debug("Stopped tracking", "branch", name)

	return nil
}

// RenameBranch renames a branch tracked by git-spice.
// This handles both, renaming the branch in the repository,
// and updating the internal state to reflect the new name.
func (s *Service) RenameBranch(ctx context.Context, oldName, newName string) error {
	oldBranch, err := s.LookupBranch(ctx, oldName)
	if err != nil {
		return fmt.Errorf("lookup %v: %w", oldName, err)
	}

	// Verify new name is not already in use.
	if _, err := s.repo.PeelToCommit(ctx, newName); err == nil {
		// TODO: A force option should override this.
		return fmt.Errorf("branch %v already exists", newName)
	}

	aboves, err := s.ListAbove(ctx, oldName)
	if err != nil {
		return fmt.Errorf("list branches above %v: %w", oldName, err)
	}

	var (
		changeForge    string
		changeMetadata json.RawMessage
	)
	if md := oldBranch.Change; md != nil {
		if f, ok := s.forges.Lookup(md.ForgeID()); ok {
			changeForge = f.ID()
			changeMetadata, err = f.MarshalChangeMetadata(md)
			if err != nil {
				return fmt.Errorf("marshal change metadata: %w", err)
			}
		}
	}

	tx := s.store.BeginBranchTx()

	// Create the new branch with the same base
	// and other state as the old branch.
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           newName,
		Base:           oldBranch.Base,
		BaseHash:       oldBranch.BaseHash,
		ChangeForge:    changeForge,
		ChangeMetadata: changeMetadata,
		UpstreamBranch: &oldBranch.UpstreamBranch,
	}); err != nil {
		return fmt.Errorf("create branch with name %v: %w", newName, err)
	}

	// Point the branches above the old branch to the new branch.
	for _, above := range aboves {
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name: above,
			Base: newName,
		}); err != nil {
			return fmt.Errorf("update branch %v to point to %v: %w", above, newName, err)
		}
		s.log.Debug("Updating upstack branch to new name", "upstack", above)
	}

	// Delete the old branch.
	if err := tx.Delete(ctx, oldName); err != nil {
		return fmt.Errorf("delete branch %v: %w", oldName, err)
	}

	// If we get here, the change will be committed successfully.
	// We can perform the Git rename and commit.
	if err := s.repo.RenameBranch(ctx, git.RenameBranchRequest{
		OldName: oldName,
		NewName: newName,
	}); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	if err := tx.Commit(ctx, fmt.Sprintf("rename %q to %q", oldName, newName)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}
	s.log.Debug("Renamed tracked name of branch", "old", oldName, "new", newName)

	return nil
}
