package github

import (
	"context"
	"fmt"

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
