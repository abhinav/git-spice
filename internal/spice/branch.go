package spice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice/state"
)

const _generatedBranchNameLimit = 32

// GenerateBranchName generates a branch name from a commit message subject.
//
// The branch name is generated by converting the subject to lowercase,
// replacing spaces with hyphens, and removing all non-alphanumeric characters.
//
// If the subject has more than 32 characters, it is truncated to 32 characters
// at word boundaries.
func GenerateBranchName(subject string) string {
	words := strings.FieldsFunc(strings.ToLower(subject), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	must.NotBeEmptyf(words, "subject must not be empty")

	var name strings.Builder
	for _, w := range words {
		newLen := name.Len() + len(w)
		needHyphen := name.Len() > 0
		if needHyphen {
			newLen++
		}

		if newLen > _generatedBranchNameLimit {
			break
		}

		if needHyphen {
			name.WriteByte('-')
		}
		for _, r := range w {
			name.WriteRune(unicode.ToLower(r))
		}
	}

	return name.String()
}

// LookupBranchResponse is the response to a LookupBranch request.
// It includes information about the tracked branch.
type LookupBranchResponse struct {
	// Base is the base branch configured
	// for the requested branch.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This may not match the current hash of the base branch.
	BaseHash git.Hash

	// Change is information about the published change
	// associated with the branch.
	//
	// This is nil if the branch hasn't been published yet.
	Change forge.ChangeMetadata

	// UpstreamBranch is the name of the upstream branch
	// or an empty string if the branch is not tracking an upstream branch.
	UpstreamBranch string

	// Head is the commit at the head of the branch.
	Head git.Hash

	// MergedBranches is a list of branches that were previously merged into trunk.
	//
	// This is used to correctly display the history of the branch.
	// TODO: use forge.ChangeID instead
	MergedBranches []string
}

// DeletedBranchError is returned when a branch was deleted out of band.
//
// This error is used to indicate that the branch does not exist,
// but its base might.
type DeletedBranchError struct {
	Name string

	Base     string
	BaseHash git.Hash
}

func (e *DeletedBranchError) Error() string {
	return fmt.Sprintf("tracked branch %v was deleted out of band", e.Name)
}

// LookupBranch returns information about a branch tracked by gs.
//
// It returns [git.ErrNotExist] if the branch is nt known to the repository,
// [state.ErrNotExist] if the branch is not tracked,
// or a [DeletedBranchError] if the branch is tracked, but was deleted out of band.
func (s *Service) LookupBranch(ctx context.Context, name string) (*LookupBranchResponse, error) {
	resp, storeErr := s.store.LookupBranch(ctx, name)
	head, gitErr := s.repo.PeelToCommit(ctx, name)

	// Handle all scenarios:
	//
	// storeErr | gitErr | Result
	// ---------|--------|-------
	// nil      | nil    | Branch exists and is tracked
	// nil      | !nil   | Branch is tracked, but was deleted out of band
	// !nil     | nil    | Branch is not tracked
	// !nil     | !nil   | Branch is not known to the repository
	if storeErr == nil && gitErr == nil {
		out := &LookupBranchResponse{
			Base:           resp.Base,
			BaseHash:       resp.BaseHash,
			UpstreamBranch: resp.UpstreamBranch,
			Head:           head,
			MergedBranches: resp.MergedBranches,
		}

		if resp.ChangeMetadata != nil {
			f := s.forge

			// It's super unliely that the branch has this metadata
			// but no forge is available to deserialize it,
			// but we'll handle it defensively anyway.
			//
			// The forge ID can also mismatch if, when we support
			// multiple forges, someone changes migrates their code
			// to a different forge after submitting a PR.
			if f == nil || f.ID() != resp.ChangeForge {
				// See if we can get the forge from the registry.
				f, _ = forge.Lookup(resp.ChangeForge)
			}

			if f != nil {
				md, err := f.UnmarshalChangeMetadata(resp.ChangeMetadata)
				if err != nil {
					s.log.Warn("Corrupt change metadata associated with branch",
						"branch", name,
						"metadata", string(resp.ChangeMetadata),
						"err", err,
					)
				} else {
					out.Change = md
				}
			}
		}

		return out, nil
	}

	// Only one of these errors is set.
	if (storeErr != nil) != (gitErr != nil) {
		// Branch is not tracked, but exists in the repository.
		if storeErr != nil {
			return nil, fmt.Errorf("untracked branch %v: %w", name, storeErr)
		}

		if !errors.Is(gitErr, git.ErrNotExist) {
			return nil, fmt.Errorf("resolve head: %w", gitErr)
		}

		// Branch is tracked, but was deleted out of band.
		return nil, &DeletedBranchError{
			Name:     name,
			Base:     resp.Base,
			BaseHash: resp.BaseHash,
		}
	}

	// Both errors are set.
	// If the branch is not known to the repository,
	// return the git error.
	if errors.Is(gitErr, git.ErrNotExist) {
		return nil, fmt.Errorf("resolve head: %w", gitErr)
	}

	// Otherwise, something went wrong. Surface both errors.
	return nil, errors.Join(
		fmt.Errorf("untracked branch %v: %w", name, storeErr),
		fmt.Errorf("resolve head: %w", gitErr),
	)
}

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
	branchNames, err := s.store.ListBranches(ctx)
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}

	branchTx := s.store.BeginBranchTx()
	for _, candidate := range branchNames {
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
	}

	if err := branchTx.Delete(ctx, name); err != nil {
		return fmt.Errorf("delete branch %v: %w", name, err)
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("untrack branch %q", name)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}

// RenameBranch renames a branch tracked by gs.
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
		if f, ok := forge.Lookup(md.ForgeID()); ok {
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

	return nil
}

// LoadBranchItem is a single branch returned by LoadBranches.
type LoadBranchItem struct {
	// Name is the name of the branch.
	Name string

	// Head is the commit at the head of the branch.
	Head git.Hash

	// Base is the name of the branch that this branch is based on.
	Base string

	// BaseHash is the last known commit hash of the base branch.
	// This may not match the current commit hash of the base branch.
	BaseHash git.Hash

	// Change is the metadata associated with the branch.
	// This is nil if the branch has not been published.
	Change forge.ChangeMetadata

	// UpstreamBranch is the name under which this branch
	// was pushed to the upstream repository.
	UpstreamBranch string

	// MergedBranches contains information about any branches,
	// which this one was based on, that have already been merged into trunk.
	MergedBranches []string
}

// LoadBranches loads all tracked branches
// and all their information as a single operation.
//
// The returned branches are sorted by name.
func (s *Service) LoadBranches(ctx context.Context) ([]LoadBranchItem, error) {
	names, err := s.store.ListBranches(ctx)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	items := make([]LoadBranchItem, 0, len(names))

	// These will be used if we encounter any branches
	// that have been deleted out of band.
	deletedBranches := make(map[string]*DeletedBranchError)
	for _, name := range names {
		resp, err := s.LookupBranch(ctx, name)
		if err != nil {
			if delErr := new(DeletedBranchError); errors.As(err, &delErr) {
				s.log.Infof("%v: removing...", delErr)
				deletedBranches[name] = delErr
				continue
			}

			return nil, fmt.Errorf("get branch %v: %w", name, err)
		}

		items = append(items, LoadBranchItem{
			Name:           name,
			Head:           resp.Head,
			Base:           resp.Base,
			BaseHash:       resp.BaseHash,
			UpstreamBranch: resp.UpstreamBranch,
			Change:         resp.Change,
			MergedBranches: resp.MergedBranches,
		})
	}

	slices.SortFunc(items, func(a, b LoadBranchItem) int {
		return strings.Compare(a.Name, b.Name)
	})

	if len(deletedBranches) == 0 {
		return items, nil
	}

	// Some of the branches we've loaded have been deleted out of band.
	// We'll delete these from the data store.
	tx := s.store.BeginBranchTx()

	// But first, we need to point the branches above deletes branches
	// to the bases of the deleted branches, or the bases of the bases,
	// and so on until we find a base that is not deleted.
	//
	// This will also update the LoadBranchItem instances
	// to reflect these changes so we're not re-reading the state.
	for i, item := range items {
		origBase := item.Base
		base, baseHash := item.Base, item.BaseHash

		delErr, deleted := deletedBranches[base]
		for deleted {
			base, baseHash = delErr.Base, delErr.BaseHash
			delErr, deleted = deletedBranches[base]
		}

		if base != origBase {
			if err := tx.Upsert(ctx, state.UpsertRequest{
				Name:     item.Name,
				Base:     base,
				BaseHash: baseHash,
			}); err != nil {
				s.log.Warn("Could not update base of branch upstack from deleted branch",
					"branch", item.Name,
					"newBase", item.Base,
					"error", err,
				)
				continue
			}

			item.Base = base
			item.BaseHash = baseHash
			items[i] = item
		}
	}

	// At this point, the deleted branches should not have any branches above them,
	// except those that we failed to update above.
	// Delete what we can, log the rest.
	for name := range deletedBranches {
		if err := tx.Delete(ctx, name); err != nil {
			s.log.Warn("Unable to delete branch", "branch", name, "err", err)
		}
	}

	if err := tx.Commit(ctx, "clean up deleted branches"); err != nil {
		s.log.Warn("Error cleaning up after deleted branched", "err", err)
	}

	return items, nil
}

func (s *Service) branchesByBase(ctx context.Context) (map[string][]string, error) {
	branchesByBase := make(map[string][]string)
	branches, err := s.LoadBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
		branchesByBase[branch.Base] = append(
			branchesByBase[branch.Base], branch.Name,
		)
	}
	return branchesByBase, nil
}

// ListAbove returns a list of branches that are immediately above the given branch.
// These are branches that have the given branch as their base.
// The slice is empty if there are no branches above the given branch.
func (s *Service) ListAbove(ctx context.Context, base string) ([]string, error) {
	var children []string
	branches, err := s.LoadBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
		if branch.Base == base {
			children = append(children, branch.Name)
		}
	}

	return children, nil
}

// ListUpstack will list all branches that are upstack from the given branch,
// including those that are upstack from the upstack branches.
// The given branch is the first element in the returned slice.
//
// The returned slice is ordered by branch position in the upstack.
// It is guaranteed that for i < j, branch[i] is not a parent of branch[j].
func (s *Service) ListUpstack(ctx context.Context, start string) ([]string, error) {
	branchesByBase, err := s.branchesByBase(ctx) // base -> [branches]
	if err != nil {
		return nil, err
	}

	var upstacks []string
	remaining := []string{start}
	for len(remaining) > 0 {
		current := remaining[0]
		remaining = remaining[1:]
		upstacks = append(upstacks, current)
		remaining = append(remaining, branchesByBase[current]...)
	}
	must.NotBeEmptyf(upstacks, "there must be at least one branch")
	must.BeEqualf(start, upstacks[0], "starting branch must be first upstack")

	return upstacks, nil
}

// FindTop returns the topmost branches in each upstack chain
// starting at the given branch.
func (s *Service) FindTop(ctx context.Context, start string) ([]string, error) {
	branchesByBase, err := s.branchesByBase(ctx) // base -> [branches]
	if err != nil {
		return nil, err
	}

	remaining := []string{start}
	var tops []string
	for len(remaining) > 0 {
		var b string
		b, remaining = remaining[0], remaining[1:]

		aboves := branchesByBase[b]
		if len(aboves) == 0 {
			// There's nothing above this branch
			// so it's a top-most branch.
			tops = append(tops, b)
		} else {
			remaining = append(remaining, aboves...)
		}
	}
	must.NotBeEmptyf(tops, "at least start branch (%v) must be in tops", start)
	return tops, nil
}

// ListDownstack lists all branches below the given branch
// in the downstack chain, not including trunk.
//
// The given branch is the first element in the returned slice,
// and the bottom-most branch is the last element.
//
// If there are no branches downstack because we're on trunk,
// or because all branches are downstack from trunk have been deleted,
// the returned slice will be nil.
func (s *Service) ListDownstack(ctx context.Context, start string) ([]string, error) {
	tx := s.store.BeginBranchTx()
	defer func() {
		if err := tx.Commit(ctx, "clean up deleted branches"); err != nil {
			s.log.Warn("Error cleaning up after deleted branched", "err", err)
		}
	}()

	var (
		downstacks []string
		previous   string
	)
	current := start
	for {
		if current == s.store.Trunk() {
			return downstacks, nil
		}

		b, err := s.LookupBranch(ctx, current)
		if err != nil {
			if delErr := new(DeletedBranchError); errors.As(err, &delErr) {
				s.log.Infof("%v", delErr)
				// If branch was deleted out of band,
				// pretend it doesn't exist,
				// and update state to point the upstack branch
				// to the base of the deleted branch.
				//
				// Leave the branch state as-is in case
				// there are other upstacks that need to be updated.
				current = delErr.Base
				if err := tx.Upsert(ctx, state.UpsertRequest{
					Name: previous,
					Base: current,
				}); err != nil {
					s.log.Warn("Could not update upstack of deleted branch",
						"branch", previous,
						"newBase", current,
						"error", err,
					)
				}
				continue
			}
			return nil, fmt.Errorf("lookup %v: %w", current, err)
		}

		downstacks = append(downstacks, current)
		previous, current = current, b.Base
	}
}

// FindBottom returns the bottom-most branch in the downstack chain
// starting at the given branch just before trunk.
//
// Returns an error if no downstack branches are found.
func (s *Service) FindBottom(ctx context.Context, start string) (string, error) {
	downstacks, err := s.ListDownstack(ctx, start)
	if err != nil {
		return "", fmt.Errorf("get downstack branches: %w", err)
	}

	if len(downstacks) == 0 {
		return "", fmt.Errorf("no downstack branches found")
	}

	return downstacks[len(downstacks)-1], nil
}

// ListStack returns the full stack of branches that the given branch is in.
//
// If the start branch has multiple upstack branches,
// all of them are included in the returned slice.
// The result is ordered by branch position in the stack
// with the bottom-most branch as the first element.
func (s *Service) ListStack(ctx context.Context, start string) ([]string, error) {
	var downstacks []string
	if start != s.store.Trunk() {
		var err error
		downstacks, err = s.ListDownstack(ctx, start)
		if err != nil {
			return nil, fmt.Errorf("get downstack branches: %w", err)
		}

		must.NotBeEmptyf(downstacks, "downstack branches must not be empty")
		must.BeEqualf(start, downstacks[0], "current branch must be first downstack")
		downstacks = downstacks[1:] // Remove current branch from list
		slices.Reverse(downstacks)
	}

	upstacks, err := s.ListUpstack(ctx, start)
	if err != nil {
		return nil, fmt.Errorf("get upstack branches: %w", err)
	}
	must.NotBeEmptyf(upstacks, "upstack branches must not be empty")
	must.BeEqualf(start, upstacks[0], "current branch must be first upstack")

	stack := make([]string, 0, len(downstacks)+len(upstacks))
	stack = append(stack, downstacks...)
	stack = append(stack, upstacks...)
	return stack, nil
}

// NonLinearStackError is returned when a stack is not linear.
// This means that a branch has more than one upstack branch.
type NonLinearStackError struct {
	Branch string
	Aboves []string
}

func (e *NonLinearStackError) Error() string {
	return fmt.Sprintf("%v has %d branches above it", e.Branch, len(e.Aboves))
}

// ListStackLinear returns the full stack of branches that the given branch is in
// but only if the stack is linear: each branch has only one upstack branch.
// If the stack is not linear, [NonLinearStackError] is returned.
//
// The returned slice is ordered by branch position in the stack
// with the bottom-most branch as the first element.
func (s *Service) ListStackLinear(ctx context.Context, start string) ([]string, error) {
	var downstacks []string
	if start != s.store.Trunk() {
		var err error
		downstacks, err = s.ListDownstack(ctx, start)
		if err != nil {
			return nil, fmt.Errorf("get downstack branches: %w", err)
		}

		must.NotBeEmptyf(downstacks, "downstack branches must not be empty")
		must.BeEqualf(start, downstacks[0], "current branch must be first downstack")
		downstacks = downstacks[1:] // Remove current branch from list
		slices.Reverse(downstacks)
	}

	branchesByBase, err := s.branchesByBase(ctx) // base -> [branches]
	if err != nil {
		return nil, err
	}

	upstacks := []string{start}
	current := start
	for aboves := branchesByBase[current]; len(aboves) > 0; {
		if len(aboves) > 1 {
			return nil, &NonLinearStackError{
				Branch: current,
				Aboves: aboves,
			}
		}

		current = aboves[0]
		upstacks = append(upstacks, current)
		aboves = branchesByBase[current]
	}

	return slices.Concat(downstacks, upstacks), nil
}
