package gs

import (
	"context"
	"fmt"
	"sort"
)

// GuessOp specifies the kind of guess operation
// that the Guesser is performing.
type GuessOp int

// List of guess operations.
const (
	GuessUnknown GuessOp = iota
	GuessRemote
	GuessTrunk
)

// Guesser attempts to make informed guesses
// about the state of a repository during initialization.
type Guesser struct {
	// Select prompts a user to select from a list of options
	// and returns the selected option.
	//
	// selected is the the option that should be selected by default
	// or an empty string if there's no preferred default.
	Select func(op GuessOp, opts []string, selected string) (string, error) // required
}

// GuessRemote attempts to guess the name of the remote
// to use for the repository.
//
// It returns an empty string if a remote was not found.
func (g *Guesser) GuessRemote(ctx context.Context, repo GitRepository) (string, error) {
	remotes, err := repo.ListRemotes(ctx)
	if err != nil {
		return "", fmt.Errorf("list remotes: %w", err)
	}

	switch len(remotes) {
	case 0:
		return "", nil
	case 1:
		return remotes[0], nil
	default:
		remote, err := g.Select(GuessRemote, remotes, "")
		if err != nil {
			return "", fmt.Errorf("prompt for remote: %w", err)
		}
		return remote, nil
	}
}

// GuessTrunk attempts to guess the name of the trunk branch of the repository.
// If remote is non-empty, it should be the name of the remote for the repository.
func (g *Guesser) GuessTrunk(ctx context.Context, repo GitRepository, remote string) (string, error) {
	defaultTrunk, err := repo.CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("determine current branch: %w", err)
	}

	// If there's a remote, and it has a default branch,
	// use that as the default trunk branch in the prompt.
	if remote != "" {
		if upstream, err := repo.DefaultBranch(ctx, remote); err == nil {
			defaultTrunk = upstream
		}
	}

	localBranches, err := repo.LocalBranches(ctx)
	if err != nil {
		return "", fmt.Errorf("list local branches: %w", err)
	}
	sort.Strings(localBranches)

	switch len(localBranches) {
	case 0:
		// There are no branches with any commits,
		// but HEAD still points to a branch.
		// This will be true for new repositories
		// without any commits only.
		return defaultTrunk, nil
	case 1:
		return localBranches[0], nil
	default:
		branch, err := g.Select(GuessTrunk, localBranches, defaultTrunk)
		if err != nil {
			return "", fmt.Errorf("prompt for trunk branch: %w", err)
		}

		return branch, nil
	}
}
