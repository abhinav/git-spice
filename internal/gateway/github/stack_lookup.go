package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// StackUpdatePullRequest is the compact pull request projection needed to
// reconcile native-stack membership.
type StackUpdatePullRequest struct {
	// ID is the pull request's GraphQL node ID.
	ID ID

	// State is the pull request lifecycle state.
	State PullRequestState

	// HeadRefName and BaseRefName identify the current provider-facing branch
	// relationship.
	HeadRefName string
	BaseRefName string

	// HeadRepositoryOwner is the login that owns the head repository.
	HeadRepositoryOwner string

	// HeadRepositoryName is the head repository name.
	HeadRepositoryName string

	// Stack is the native stack containing the pull request.
	// It is nil when the pull request was not stacked in the initial query or
	// when its referenced stack stopped resolving before the follow-up query.
	Stack *PullRequestStack
}

// PullRequestsForStackUpdate loads pull request eligibility and native-stack
// membership in input order.
// A nil result entry means that GitHub did not find the corresponding pull
// request.
// When Stack is non-nil, it includes all open members in base-up order.
//
// The result combines two API snapshots because GitHub exposes stack identity
// on the pull request and ordered membership on a separate stack node.
// If a referenced stack stops resolving between those requests, the pull
// request result remains present with Stack nil.
//
// GitHub exposes PullRequestStack as a GraphQL node, so this operation loads
// each unique stack once rather than repeating its entries for every member.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequeststack.
func (c *Gateway) PullRequestsForStackUpdate(
	ctx context.Context,
	owner string,
	repo string,
	numbers []int,
) ([]*StackUpdatePullRequest, error) {
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
	//       id state headRefName baseRefName
	//       headRepository { owner { login } name }
	//       stack { id }
	//     }
	//     pr1: pullRequest(number: $pr1) {
	//       state
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
			"%[1]s:pullRequest(number: $%[1]s){id,state,headRefName,baseRefName,headRepository{owner{login},name},stack{id}}",
			alias,
		)
		variables[alias] = number
	}

	var result struct {
		Repository map[string]*struct {
			ID             ID               `json:"id"`
			State          PullRequestState `json:"state"`
			HeadRefName    string           `json:"headRefName"`
			BaseRefName    string           `json:"baseRefName"`
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
	query := compactGraphQL(fmt.Sprintf(`
		query(%s){
			repository(owner: $owner, name: $repo){%s}
		}
	`, variableDefinitions.String(), selections.String()))
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull requests for stack update: %w", err)
	}

	pullRequests := make([]*StackUpdatePullRequest, len(numbers))
	pullRequestsAwaitingStackByID := make(map[ID][]*StackUpdatePullRequest)
	var stackIDsToResolve []ID
	for i := range numbers {
		res := result.Repository["pr"+strconv.Itoa(i)]
		if res == nil {
			continue
		}

		pullRequest := &StackUpdatePullRequest{
			ID:                  res.ID,
			State:               res.State,
			HeadRefName:         res.HeadRefName,
			BaseRefName:         res.BaseRefName,
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

	// Resolve the ordered open members for every unique stack ID in one query.
	// A node may disappear after the pull request query; in that case, leaving
	// Stack nil preserves the best snapshot this non-atomic operation obtained.
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

// pullRequestStacksByID loads every ordered open member of each native stack.
// The first page for all stacks is batched; stacks larger than one GraphQL page
// are completed with per-stack continuation queries.
func (c *Gateway) pullRequestStacksByID(
	ctx context.Context,
	stackIDs []ID,
) (map[ID]*PullRequestStack, error) {
	var result struct {
		Nodes []*struct {
			ID      ID                                `json:"id"`
			Number  int                               `json:"number"`
			Entries pullRequestStackEntriesConnection `json:"entries"`
		} `json:"nodes"`
	}
	// Build one batched query for the first page of every referenced stack:
	//
	// query($ids:[ID!]!) {
	//   nodes(ids: $ids) {
	//     ... on PullRequestStack {
	//       id number
	//       entries(first: 100) {
	//         nodes { pullRequest {
	//           number state
	//           mergeQueueEntry { id }
	//           autoMergeRequest { enabledAt }
	//         } }
	//         pageInfo { endCursor hasNextPage }
	//       }
	//     }
	//   }
	// }
	query := compactGraphQL(`
		query($ids:[ID!]!){
			nodes(ids: $ids){
				... on PullRequestStack{
					id,number,
					entries(first: 100){
						nodes{pullRequest{
							number,state,mergeQueueEntry{id},autoMergeRequest{enabledAt}
						}},
						pageInfo{endCursor,hasNextPage}
					}
				}
			}
		}
	`)
	if err := c.executeGQL(ctx, query, struct {
		IDs []ID `json:"ids"`
	}{stackIDs}, &result); err != nil {
		return nil, fmt.Errorf("query pull request stacks: %w", err)
	}

	stacksByID := make(map[ID]*PullRequestStack, len(result.Nodes))
	for _, res := range result.Nodes {
		if res == nil {
			continue
		}

		stack := &PullRequestStack{Number: res.Number}
		entries := &res.Entries
		for pageNum := 1; ; pageNum++ {
			for _, entry := range entries.Nodes {
				pullRequest := entry.PullRequest
				if pullRequest == nil || pullRequest.State != PullRequestStateOpen {
					continue
				}
				stack.Members = append(stack.Members, PullRequestStackMember{
					Number: pullRequest.Number,
					Locked: pullRequest.MergeQueueEntry != nil ||
						pullRequest.AutoMergeRequest != nil,
				})
			}
			if !entries.PageInfo.HasNextPage {
				break
			}
			if entries.PageInfo.EndCursor == "" {
				return nil, fmt.Errorf(
					"query pull request stack %d entries: page %d has no end cursor",
					res.Number,
					pageNum,
				)
			}

			nextEntries, err := c.pullRequestStackEntriesPage(
				ctx,
				res.ID,
				entries.PageInfo.EndCursor,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"query pull request stack %d entries page %d: %w",
					res.Number,
					pageNum+1,
					err,
				)
			}
			if nextEntries == nil {
				stack = nil
				break
			}
			entries = nextEntries
		}
		if stack != nil {
			stacksByID[res.ID] = stack
		}
	}

	return stacksByID, nil
}

// pullRequestStackEntriesPage loads one continuation page for a stack.
// It returns nil when the stack node no longer resolves.
func (c *Gateway) pullRequestStackEntriesPage(
	ctx context.Context,
	stackID ID,
	after string,
) (*pullRequestStackEntriesConnection, error) {
	var result struct {
		Node *struct {
			Entries pullRequestStackEntriesConnection `json:"entries"`
		} `json:"node"`
	}
	// Build a continuation query for one stack's remaining entries:
	//
	// query($after:String!$id:ID!) {
	//   node(id: $id) {
	//     ... on PullRequestStack {
	//       entries(first: 100, after: $after) {
	//         nodes { pullRequest {
	//           number state
	//           mergeQueueEntry { id }
	//           autoMergeRequest { enabledAt }
	//         } }
	//         pageInfo { endCursor hasNextPage }
	//       }
	//     }
	//   }
	// }
	query := compactGraphQL(`
		query($after:String!$id:ID!){
			node(id: $id){
				... on PullRequestStack{
					entries(first: 100, after: $after){
						nodes{pullRequest{
							number,state,mergeQueueEntry{id},autoMergeRequest{enabledAt}
						}},
						pageInfo{endCursor,hasNextPage}
					}
				}
			}
		}
	`)
	variables := struct {
		After string `json:"after"`
		ID    ID     `json:"id"`
	}{after, stackID}
	if err := c.executeGQL(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	if result.Node == nil {
		return nil, nil
	}
	return &result.Node.Entries, nil
}

type pullRequestStackEntriesConnection struct {
	Nodes []struct {
		PullRequest *struct {
			Number           int              `json:"number"`
			State            PullRequestState `json:"state"`
			MergeQueueEntry  *struct{}        `json:"mergeQueueEntry"`
			AutoMergeRequest *struct{}        `json:"autoMergeRequest"`
		} `json:"pullRequest"`
	} `json:"nodes"`
	PageInfo struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
}
