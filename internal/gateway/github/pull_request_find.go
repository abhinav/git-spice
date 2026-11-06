package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PullRequestBranchMatch is the compact pull request projection used to match
// branch names during repository synchronization.
// See https://docs.github.com/en/graphql/reference/objects#pullrequest.
type PullRequestBranchMatch struct {
	// ID is the pull request's GraphQL node ID.
	ID ID `json:"id"`

	// Number is the repository-local pull request number.
	Number int `json:"number"`

	// State is the pull request lifecycle state.
	State PullRequestState `json:"state"`

	// HeadRefOID is the Git object ID at the head of the pull request.
	HeadRefOID string `json:"headRefOid"`

	// HeadRepository identifies the repository containing the head branch.
	HeadRepository struct {
		// Owner identifies the account that owns the head repository.
		Owner struct {
			// Login is the owner's GitHub login.
			Login string `json:"login"`
		} `json:"owner"`

		// Name is the head repository name.
		Name string `json:"name"`
	} `json:"headRepository"`
}

// FindPullRequestsByBranchesRequest describes one batched branch lookup.
type FindPullRequestsByBranchesRequest struct {
	// Owner is the login that owns the target repository.
	Owner string // required

	// Repo is the target repository name.
	Repo string // required

	// Branches lists head branch names in result order.
	Branches []string
}

// FindPullRequestsByBranches finds recent pull requests for multiple head branches.
// The returned slice has the same length and order as req.Branches.
// Each inner slice preserves GitHub's updated-at ordering for that branch.
// An empty branch list returns nil without making a request.
func (c *Gateway) FindPullRequestsByBranches(
	ctx context.Context,
	req *FindPullRequestsByBranchesRequest,
) ([][]*PullRequestBranchMatch, error) {
	if len(req.Branches) == 0 {
		return nil, nil
	}

	variables := make(map[string]any, len(req.Branches)+4)
	variables["limit"] = 10
	variables["owner"] = req.Owner
	variables["repo"] = req.Repo
	variables["states"] = []PullRequestState{
		PullRequestStateOpen,
		PullRequestStateClosed,
		PullRequestStateMerged,
	}

	// Build one connection per input branch:
	//
	// query($branch0:String!$branch1:String!...) {
	//   repository(owner: $owner, name: $repo) {
	//     branch0: pullRequests(headRefName: $branch0, ...) { nodes { ... } }
	//     branch1: pullRequests(headRefName: $branch1, ...) { nodes { ... } }
	//   }
	// }
	var variableDefinitions strings.Builder
	var selections strings.Builder
	for i, branch := range req.Branches {
		alias := "branch" + strconv.Itoa(i)
		fmt.Fprintf(&variableDefinitions, "$%s:String!", alias)
		if i > 0 {
			selections.WriteByte(',')
		}
		fmt.Fprintf(
			&selections,
			"%[1]s:pullRequests(first: $limit, headRefName: $%[1]s, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}){"+
				"nodes{id,number,state,headRefOid,headRepository{owner{login},name}}}",
			alias,
		)
		variables[alias] = branch
	}

	query := compactGraphQL(
		"query(" + variableDefinitions.String() +
			"$limit:Int!$owner:String!$repo:String!$states:[PullRequestState!]!){" +
			"repository(owner: $owner, name: $repo){" + selections.String() + "}}",
	)
	var result struct {
		Repository map[string]struct {
			Nodes []*PullRequestBranchMatch `json:"nodes"`
		} `json:"repository"`
	}
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull requests by branch: %w", err)
	}

	pullRequests := make([][]*PullRequestBranchMatch, len(req.Branches))
	for i := range req.Branches {
		pullRequests[i] = result.Repository["branch"+strconv.Itoa(i)].Nodes
	}
	return pullRequests, nil
}

// FindPullRequests finds recent pull requests with the requested head branch.
func (c *Gateway) FindPullRequests(ctx context.Context, owner, repo, branch string, limit int, states []PullRequestState) ([]*PullRequest, error) {
	var result struct {
		Repository struct {
			PullRequests struct {
				Nodes []*PullRequest `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	variables := struct {
		Branch string             `json:"branch"`
		Limit  int                `json:"limit"`
		Owner  string             `json:"owner"`
		Repo   string             `json:"repo"`
		States []PullRequestState `json:"states"`
	}{branch, limit, owner, repo, states}
	query := compactGraphQL(`
		query($branch:String!$limit:Int!$owner:String!$repo:String!$states:[PullRequestState!]!){
			repository(owner: $owner, name: $repo){
				pullRequests(first: $limit, headRefName: $branch, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}){
					nodes{` + pullRequestFields + `}
				}
			}
		}
	`)
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull requests: %w", err)
	}
	return result.Repository.PullRequests.Nodes, nil
}

// PullRequest loads a pull request by repository and number.
func (c *Gateway) PullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	var result struct {
		Repository struct {
			PullRequest *PullRequest `json:"pullRequest"`
		} `json:"repository"`
	}
	variables := struct {
		Number int    `json:"number"`
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
	}{number, owner, repo}
	query := compactGraphQL(`
		query($number:Int!$owner:String!$repo:String!){
			repository(owner: $owner, name: $repo){
				pullRequest(number: $number){` + pullRequestFields + `}
			}
		}
	`)
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull request: %w", err)
	}
	return result.Repository.PullRequest, nil
}
