package spice

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice/state"
)

// ForgetBranch stops tracking a branch,
// updating the upstacks for it to point to its base.
func (s *Service) ForgetBranch(ctx context.Context, name string) error {
	// This does not use LookupBranch because we don't care if the branch
	// doesn't actually exist, we just want to update the upstacks.
	branchTx := s.store.BeginBranchTx()
	branches, err := branchTx.LookupBranches(ctx, []string{name})
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("lookup branch: %w", err)
	}
	branch := branches[0]

	// Similarly, this doesn't use ListAbove
	// because we don't want the deleted branch to be removed yet.
	names, err := sliceutil.CollectErr(s.store.ListBranches(ctx))
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	branches, err = branchTx.LookupBranches(ctx, names)
	if err != nil {
		return fmt.Errorf("lookup branches: %w", err)
	}
	var updates []state.UpsertRequest
	for i, candidate := range names {

		if candidate == name {
			continue
		}

		info := branches[i]

		if info.Base != name {
			continue
		}

		updates = append(updates, state.UpsertRequest{
			Name:     candidate,
			Base:     branch.Base,
			BaseHash: branch.BaseHash,
		})
	}
	if err := branchTx.UpsertMany(ctx, updates); err != nil {
		return fmt.Errorf("reparent branches onto %q: %w", branch.Base, err)
	}
	for _, update := range updates {
		s.log.Debug("Updating upstack branch to new base",
			"branch", update.Name,
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
	if _, err := s.LookupBranch(ctx, oldName); err != nil {
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

	tx := s.store.BeginBranchTx()
	if err := tx.Rename(ctx, oldName, newName); err != nil {
		return fmt.Errorf("rename tracked branch: %w", err)
	}

	// Point the branches above the old branch to the new branch.
	updates := make([]state.UpsertRequest, len(aboves))
	for i, above := range aboves {
		updates[i] = state.UpsertRequest{
			Name: above,
			Base: newName,
		}
	}
	if err := tx.UpsertMany(ctx, updates); err != nil {
		return fmt.Errorf("reparent branches onto %q: %w", newName, err)
	}
	for _, above := range aboves {
		s.log.Debug("Updating upstack branch to new name", "upstack", above)
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
