package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// MergeRangePullRequest is the compact pull request projection needed to
// validate an asynchronous range merge.
type MergeRangePullRequest struct {
	// State is the pull request lifecycle state.
	State PullRequestState

	// IsDraft reports whether the pull request is a draft.
	IsDraft bool

	// BaseRefName is the target branch name.
	BaseRefName string

	// HeadRefName is the source branch name.
	HeadRefName string

	// HeadRefOID is the Git object ID at the head of the pull request.
	HeadRefOID string

	// HeadRepositoryOwner is the login that owns the head repository.
	HeadRepositoryOwner string

	// HeadRepositoryName is the head repository name.
	HeadRepositoryName string

	// Stack is the native stack containing the pull request.
	// It is nil when the pull request is not stacked or when its referenced
	// stack stopped resolving before the follow-up query.
	Stack *PullRequestStack
}

// PullRequestsForMergeRange loads merge validation state and native-stack
// membership in input order.
// A nil result entry means that GitHub did not find the corresponding pull
// request.
// When Stack is non-nil, it includes all members in base-up order.
//
// The result combines two API reads because GitHub exposes stack identity on
// the pull request and ordered membership on a separate stack node.
// If a referenced stack stops resolving between those requests, the pull
// request result remains present with Stack nil.
//
// GitHub exposes PullRequestStack as a GraphQL node, so this operation loads
// each unique stack once rather than repeating its entries for every member.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequeststack.
func (c *Gateway) PullRequestsForMergeRange(
	ctx context.Context,
	owner string,
	repo string,
	numbers []int,
) ([]*MergeRangePullRequest, error) {
	if len(numbers) == 0 {
		return nil, nil
	}

	variables := make(map[string]any, len(numbers)+2)
	variables["owner"] = owner
	variables["repo"] = repo

	// Build one aliased repository selection per pull request number:
	//
	// query($owner:String!$repo:String!$pr0:Int!$pr1:Int!) {
	//   repository(owner: $owner, name: $repo) {
	//     pr0: pullRequest(number: $pr0) {
	//       state
	//       isDraft
	//       baseRefName
	//       headRefName
	//       headRefOid
	//       headRepository { owner { login } name }
	//       stack { id }
	//     }
	//     pr1: pullRequest(number: $pr1) {
	//       state
	//       isDraft
	//       baseRefName
	//       headRefName
	//       headRefOid
	//       headRepository { owner { login } name }
	//       stack { id }
	//     }
	//   }
	// }
	var variableDefinitions strings.Builder
	variableDefinitions.WriteString("$owner:String!,$repo:String!")
	var selections strings.Builder

	// Indexed aliases preserve one response slot for every input number,
	// including duplicate numbers and pull requests GitHub does not find.
	for i, number := range numbers {
		alias := "pr" + strconv.Itoa(i)
		fmt.Fprintf(&variableDefinitions, ",$%s:Int!", alias)
		if i > 0 {
			selections.WriteByte(',')
		}
		fmt.Fprintf(
			&selections,
			"%[1]s:pullRequest(number: $%[1]s){state,isDraft,baseRefName,headRefName,headRefOid,headRepository{owner{login},name},stack{id}}",
			alias,
		)
		variables[alias] = number
	}

	var result struct {
		Repository map[string]*struct {
			State          PullRequestState `json:"state"`
			IsDraft        bool             `json:"isDraft"`
			BaseRefName    string           `json:"baseRefName"`
			HeadRefName    string           `json:"headRefName"`
			HeadRefOID     string           `json:"headRefOid"`
			HeadRepository struct {
				Owner struct {
					Login string `json:"login"`
				} `json:"owner"`
				Name string `json:"name"`
			} `json:"headRepository"`
			Stack *struct {
				ID ID `json:"id"`
			} `json:"stack"`
		} `json:"repository"`
	}
	query := compactGraphQL(
		fmt.Sprintf(`
			query(%s){
				repository(owner: $owner, name: $repo){%s}
			}
		`, variableDefinitions.String(), selections.String()),
	)
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull requests for merge range: %w", err)
	}

	pullRequests := make([]*MergeRangePullRequest, len(numbers))
	pullRequestsAwaitingStackByID := make(map[ID][]*MergeRangePullRequest)
	var stackIDsToResolve []ID
	for i := range numbers {
		res := result.Repository["pr"+strconv.Itoa(i)]
		if res == nil {
			continue
		}

		pullRequest := &MergeRangePullRequest{
			State:               res.State,
			IsDraft:             res.IsDraft,
			BaseRefName:         res.BaseRefName,
			HeadRefName:         res.HeadRefName,
			HeadRefOID:          res.HeadRefOID,
			HeadRepositoryOwner: res.HeadRepository.Owner.Login,
			HeadRepositoryName:  res.HeadRepository.Name,
		}
		pullRequests[i] = pullRequest
		if res.Stack == nil {
			continue
		}

		stackID := res.Stack.ID
		if _, seen := pullRequestsAwaitingStackByID[stackID]; !seen {
			stackIDsToResolve = append(stackIDsToResolve, stackID)
		}
		pullRequestsAwaitingStackByID[stackID] = append(
			pullRequestsAwaitingStackByID[stackID],
			pullRequest,
		)
	}
	if len(stackIDsToResolve) == 0 {
		return pullRequests, nil
	}

	// Resolve the ordered members for every unique stack ID in one query.
	// A node may disappear after the pull request query; in that case, leaving
	// Stack nil preserves the best remote view this non-atomic operation obtained.
	resolvedStacksByID, err := c.pullRequestStacksByID(ctx, stackIDsToResolve)
	if err != nil {
		return nil, err
	}
	for stackID, awaitingPullRequests := range pullRequestsAwaitingStackByID {
		resolvedStack, ok := resolvedStacksByID[stackID]
		if !ok {
			continue
		}
		for _, pullRequest := range awaitingPullRequests {
			pullRequest.Stack = resolvedStack
		}
	}
	return pullRequests, nil
}
