package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
)

// currentBranchForNavigation reports the branch that navigation commands
// should treat as the user's starting point.
//
// Detached HEAD is accepted only when it points at exactly one local branch.
// That keeps Git's detached state visible to command logic
// while still letting navigation commands reattach or move from
// `gs ... --detach` output.
func currentBranchForNavigation(
	ctx context.Context,
	wt *git.Worktree,
) (branch string, attached bool, err error) {
	current, err := wt.CurrentBranch(ctx)
	if err == nil {
		return current, true, nil
	}
	if !errors.Is(err, git.ErrDetachedHead) {
		return "", false, fmt.Errorf("get current branch: %w", err)
	}

	branches, err := sliceutil.CollectErr(wt.BranchesAtHead(ctx))
	if err != nil {
		return "", false, fmt.Errorf("get current branch: %w", err)
	}
	switch len(branches) {
	case 0:
		return "", false, errors.New(
			"detached HEAD does not point at a local branch")
	case 1:
		return branches[0], false, nil
	default:
		return "", false, fmt.Errorf(
			"detached HEAD points at multiple local branches: %s",
			strings.Join(branches, ", "),
		)
	}
}
