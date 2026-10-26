package github

import (
	"context"
	"fmt"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git"
)

// GitHub documents a 500,000-node limit per GraphQL call and accepts at most
// 100 nodes per connection.
// This query requests 10 pull requests per branch,
// so a batch of 100 branches requests at most 1,000 pull request nodes
// before the shallow head-repository projection.
// See https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api#node-limit.
const maxBranchesPerMatchRequest = 100

// MatchChangesToBranches finds compact change information for branches.
// Results have the same length and order as branches.
// A failed batch sets a branch-specific Err on every result it covers.
func (r *Repository) MatchChangesToBranches(
	ctx context.Context,
	branches []string,
	opts *forge.MatchChangesToBranchesOptions,
) []*forge.MatchChangesToBranchesResult {
	results := make([]*forge.MatchChangesToBranchesResult, len(branches))
	for i := range results {
		results[i] = new(forge.MatchChangesToBranchesResult)
	}

	pushRepository := r.repositoryID()
	if opts != nil && opts.PushRepository != nil {
		pushRepository = mustRepositoryID(opts.PushRepository)
	}

	var wg sync.WaitGroup
	for batchStart := 0; batchStart < len(branches); batchStart += maxBranchesPerMatchRequest {
		batchEnd := min(batchStart+maxBranchesPerMatchRequest, len(branches))
		wg.Go(func() {
			batchBranches := branches[batchStart:batchEnd]
			pullRequests, err := r.gateway.FindPullRequestsByBranches(
				ctx,
				&github.FindPullRequestsByBranchesRequest{
					Owner:    r.owner,
					Repo:     r.repo,
					Branches: batchBranches,
				},
			)
			if err != nil {
				for i, branch := range batchBranches {
					results[batchStart+i].Err = fmt.Errorf(
						"match change to %q: %w", branch, err,
					)
				}
				return
			}

			for batchIndex, nodes := range pullRequests {
				result := results[batchStart+batchIndex]
				for _, node := range nodes {
					// GitHub matches only the branch name, so a pull request from
					// another fork may appear in the connection.
					if node.HeadRepository.Owner.Login != pushRepository.owner ||
						node.HeadRepository.Name != pushRepository.name {
						continue
					}
					result.Changes = append(result.Changes, &forge.MatchedBranchChange{
						ID: &PR{
							Number: node.Number,
							GQLID:  node.ID,
						},
						State:    forgeChangeState(node.State),
						HeadHash: git.Hash(node.HeadRefOID),
					})
				}
			}
		})
	}
	wg.Wait()

	return results
}
