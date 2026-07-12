package github

import (
	"context"
	"fmt"
	"time"
)

// Comment is GitHub's wire representation of one pull request comment.
type Comment struct {
	// ID is the comment's GraphQL node ID.
	ID ID `json:"id"`

	// Body is the comment text.
	Body string `json:"body"`

	// URL is GitHub's browser URL for the comment.
	URL string `json:"url"`

	// ViewerCanUpdate reports whether the authenticated viewer may edit the comment.
	ViewerCanUpdate bool `json:"viewerCanUpdate"`

	// ViewerDidAuthor reports whether the authenticated viewer authored the comment.
	ViewerDidAuthor bool `json:"viewerDidAuthor"`

	// CreatedAt is the time GitHub created the comment.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the time GitHub last updated the comment.
	UpdatedAt time.Time `json:"updatedAt"`
}

// CommentsPage is one page from a pull request's comments connection.
type CommentsPage struct {
	// Nodes contains the comments on this page.
	Nodes []*Comment

	// EndCursor is usable as after when HasNextPage is true.
	EndCursor string

	// HasNextPage reports whether another page follows EndCursor.
	HasNextPage bool
}

// PullRequestComments loads one page with first entries.
// A nil after selects the first page; a non-nil after continues after that
// cursor. First must be from 1 through 100.
func (c *Gateway) PullRequestComments(ctx context.Context, id ID, first int, after *string) (*CommentsPage, error) {
	var result struct {
		Node struct {
			Comments struct {
				PageInfo struct {
					EndCursor   string `json:"endCursor"`
					HasNextPage bool   `json:"hasNextPage"`
				} `json:"pageInfo"`
				Nodes []*Comment `json:"nodes"`
			} `json:"comments"`
		} `json:"node"`
	}
	variables := struct {
		After *string `json:"after"`
		First int     `json:"first"`
		ID    ID      `json:"id"`
	}{after, first, id}
	afterType := "String"
	if after != nil {
		afterType = "String!"
	}
	query := compactGraphQL(`
		query($after:` + afterType + `$first:Int!$id:ID!){
			node(id: $id){
				... on PullRequest{
					comments(first: $first, after: $after){
						pageInfo{endCursor,hasNextPage},
						nodes{id,body,url,viewerCanUpdate,viewerDidAuthor,createdAt,updatedAt}
					}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("query pull request comments: %w", err)
	}
	comments := result.Node.Comments
	return &CommentsPage{Nodes: comments.Nodes, EndCursor: comments.PageInfo.EndCursor, HasNextPage: comments.PageInfo.HasNextPage}, nil
}

// AddedComment identifies a comment returned after creation.
type AddedComment struct {
	// ID is the new comment's GraphQL node ID.
	ID ID `json:"id"`

	// URL is GitHub's browser URL for the created comment.
	URL string `json:"url"`
}

// AddComment adds a comment to a node.
func (c *Gateway) AddComment(ctx context.Context, subjectID ID, body string) (*AddedComment, error) {
	var result struct {
		AddComment struct {
			CommentEdge struct {
				Node *AddedComment `json:"node"`
			} `json:"commentEdge"`
		} `json:"addComment"`
	}
	mutation := compactGraphQL(`
		mutation($input:AddCommentInput!){
			addComment(input: $input){commentEdge{node{id,url}}}
		}
	`)
	if err := c.mutate(ctx, mutation, struct {
		SubjectID ID     `json:"subjectId"`
		Body      string `json:"body"`
	}{subjectID, body}, &result); err != nil {
		return nil, err
	}
	return result.AddComment.CommentEdge.Node, nil
}

// UpdateIssueComment updates an issue comment body.
func (c *Gateway) UpdateIssueComment(ctx context.Context, id ID, body string) error {
	mutation := compactGraphQL(`
		mutation($input:UpdateIssueCommentInput!){
			updateIssueComment(input: $input){issueComment{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ID   ID     `json:"id"`
		Body string `json:"body"`
	}{id, body}, &struct{}{})
}

// DeleteIssueComment deletes an issue comment.
func (c *Gateway) DeleteIssueComment(ctx context.Context, id ID) error {
	mutation := compactGraphQL(`
		mutation($input:DeleteIssueCommentInput!){
			deleteIssueComment(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ID ID `json:"id"`
	}{id}, &struct{}{})
}
