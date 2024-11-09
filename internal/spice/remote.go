package spice

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
)

// UnusedBranchName returns a branch name that is not in use in the given remote,
// based on the given branch name.
func (s *Service) UnusedBranchName(ctx context.Context, remote, branch string) (string, error) {
	// ListRemoteRefs makes a network call, so we lookups in batches.
	const batchSize = 5

	candidates := make([]string, 0, batchSize)
	sawBranches := make(map[string]struct{})
	for batchStart := 1; ; batchStart += batchSize {
		candidates = candidates[:0]
		clear(sawBranches)

		for i := batchStart; i < batchStart+batchSize; i++ {
			name := branch
			if i > 1 {
				name += fmt.Sprintf("-%d", i)
			}
			candidates = append(candidates, name)
		}

		opts := git.ListRemoteRefsOptions{
			Heads:    true,
			Patterns: candidates,
		}

		for remoteRef, err := range s.repo.ListRemoteRefs(ctx, remote, &opts) {
			if err != nil {
				return "", fmt.Errorf("list remote refs: %w", err)
			}

			name := strings.TrimPrefix(remoteRef.Name, "refs/heads/")
			sawBranches[name] = struct{}{}
		}

		for _, name := range candidates {
			if _, ok := sawBranches[name]; !ok {
				return name, nil
			}
		}
	}
}
