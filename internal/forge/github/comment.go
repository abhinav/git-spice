package github

import (
	"context"
	"fmt"
	"iter"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// PRComment is a ChangeCommentID for a GitHub PR comment.
type PRComment struct {
	GQLID githubv4.ID `json:"gqlID,omitempty"`
	URL   string      `json:"url,omitempty"`
}

var _ forge.ChangeCommentID = (*PRComment)(nil)

func mustPRComment(id forge.ChangeCommentID) *PRComment {
	if id == nil {
		return nil
	}

	prc, ok := id.(*PRComment)
	if !ok {
		panic(fmt.Sprintf("unexpected PR comment type: %T", id))
	}
	return prc
}

func (c *PRComment) String() string {
	return c.URL
}

// PostChangeComment posts a new comment on a PR.
func (f *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	gqlID, err := f.graphQLID(ctx, mustPR(id))
	if err != nil {
		return nil, err
	}

	var m struct {
		AddComment struct {
			CommentEdge struct {
				Node struct {
					ID  githubv4.ID `graphql:"id"`
					URL string      `graphql:"url"`
				} `graphql:"node"`
			} `graphql:"commentEdge"`
		} `graphql:"addComment(input: $input)"`
	}

	input := githubv4.AddCommentInput{
		SubjectID: gqlID,
		Body:      githubv4.String(markdown),
	}

	if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	n := m.AddComment.CommentEdge.Node
	f.log.Debug("Posted comment", "url", n.URL)
	return &PRComment{
		GQLID: n.ID,
		URL:   n.URL,
	}, nil
}

// UpdateChangeComment updates the contents of an existing comment on a PR.
func (f *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	cid := mustPRComment(id)
	gqlID := cid.GQLID

	var m struct {
		UpdateIssueComment struct {
			IssueComment struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"issueComment"`
		} `graphql:"updateIssueComment(input: $input)"`
	}

	input := githubv4.UpdateIssueCommentInput{
		Body: githubv4.String(markdown),
		ID:   gqlID,
	}
	if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("update comment: %w", err)
	}

	f.log.Debug("Updated comment", "url", cid.URL)
	return nil
}

// DeleteChangeComment deletes an existing comment on a PR.
func (f *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	// DeleteChangeComment isn't part of the forge.Repository interface.
	// It's just nice to have to clean up after the integration test.
	cid := mustPRComment(id)
	gqlID := cid.GQLID

	var m struct {
		DeleteIssueComment struct {
			ClientMutationID githubv4.String `graphql:"clientMutationId"`
		} `graphql:"deleteIssueComment(input: $input)"`
	}

	input := githubv4.DeleteIssueCommentInput{ID: gqlID}
	if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	return nil
}

// There isn't a way to filter comments by contents server-side,
// so we'll be doing that client-side.
// GitHub's GraphQL API rate limits based on the number of nodes queried,
// so we'll be fetching comments in pages of 10 instead of an obnoxious number.
//
// Since our comment will usually be among the first few comments,
// that, plus the ascending order of comments, should make this good enough.
var _listChangeCommentsPageSize = 10 // var for testing

// ListChangeComments lists comments on a PR,
// optionally applying the given filtering options.
func (f *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	type commentNode struct {
		ID   githubv4.ID `graphql:"id"`
		Body string      `graphql:"body"`
		URL  string      `graphql:"url"`

		ViewerCanUpdate bool `graphql:"viewerCanUpdate"`
		ViewerDidAuthor bool `graphql:"viewerDidAuthor"`

		CreatedAt githubv4.DateTime `graphql:"createdAt"`
		UpdatedAt githubv4.DateTime `graphql:"updatedAt"`
	}

	var filters []func(commentNode) (keep bool)
	if options != nil {
		if len(options.BodyMatchesAll) != 0 {
			for _, re := range options.BodyMatchesAll {
				filters = append(filters, func(node commentNode) bool {
					return re.MatchString(node.Body)
				})
			}
		}
		if options.CanUpdate {
			filters = append(filters, func(node commentNode) bool {
				return node.ViewerCanUpdate
			})
		}
	}

	gqlID, err := f.graphQLID(ctx, mustPR(id))
	if err != nil {
		return func(yield func(*forge.ListChangeCommentItem, error) bool) {
			yield(nil, err)
		}
	}

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		var q struct {
			Node struct {
				PullRequest struct {
					Comments struct {
						PageInfo struct {
							EndCursor   githubv4.String `graphql:"endCursor"`
							HasNextPage bool            `graphql:"hasNextPage"`
						} `graphql:"pageInfo"`

						Nodes []commentNode `graphql:"nodes"`
					} `graphql:"comments(first: $first, after: $after)"`
				} `graphql:"... on PullRequest"`
			} `graphql:"node(id: $id)"`
		}

		variables := map[string]any{
			"id":    gqlID,
			"first": githubv4.Int(_listChangeCommentsPageSize),
			"after": (*githubv4.String)(nil),
		}

		for pageNum := 1; true; pageNum++ {
			if err := f.client.Query(ctx, &q, variables); err != nil {
				yield(nil, fmt.Errorf("list comments (page %d): %w", pageNum, err))
				return
			}

			for _, node := range q.Node.PullRequest.Comments.Nodes {
				match := true
				for _, filter := range filters {
					if !filter(node) {
						match = false
						break
					}
				}
				if !match {
					continue
				}

				item := &forge.ListChangeCommentItem{
					ID: &PRComment{
						GQLID: node.ID,
						URL:   node.URL,
					},
					Body: node.Body,
				}

				if !yield(item, nil) {
					return
				}
			}

			if !q.Node.PullRequest.Comments.PageInfo.HasNextPage {
				return
			}

			variables["after"] = q.Node.PullRequest.Comments.PageInfo.EndCursor
		}
	}
}
