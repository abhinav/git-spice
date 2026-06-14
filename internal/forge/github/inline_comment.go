package github

import (
	"context"
	"fmt"
	"time"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

var _ forge.WithInlineComments = (*Repository)(nil)

// ListInlineComments lists inline/review comments on a PR
// by querying the reviewThreads connection.
func (r *Repository) ListInlineComments(
	ctx context.Context,
	id forge.ChangeID,
) ([]*forge.InlineComment, error) {
	pr := mustPR(id)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return nil, err
	}

	type commentNode struct {
		ID     githubv4.ID `graphql:"id"`
		Body   string      `graphql:"body"`
		URL    string      `graphql:"url"`
		Author struct {
			Login string `graphql:"login"`
		} `graphql:"author"`
		CreatedAt githubv4.DateTime `graphql:"createdAt"`
		Outdated  bool              `graphql:"outdated"`
	}

	type threadNode struct {
		ID         githubv4.ID `graphql:"id"`
		IsResolved bool        `graphql:"isResolved"`
		Path       string      `graphql:"path"`
		Line       *int        `graphql:"line"`
		Comments   struct {
			Nodes []commentNode `graphql:"nodes"`
		} `graphql:"comments(first: 100)"`
	}

	var q struct {
		Node struct {
			PullRequest struct {
				ReviewThreads struct {
					PageInfo struct {
						HasNextPage bool            `graphql:"hasNextPage"`
						EndCursor   githubv4.String `graphql:"endCursor"`
					} `graphql:"pageInfo"`
					Nodes []threadNode `graphql:"nodes"`
				} `graphql:"reviewThreads(first: $first, after: $after)"`
			} `graphql:"... on PullRequest"`
		} `graphql:"node(id: $id)"`
	}

	variables := map[string]any{
		"id":    gqlID,
		"first": githubv4.Int(100),
		"after": (*githubv4.String)(nil),
	}

	var comments []*forge.InlineComment
	for {
		if err := r.client.Query(ctx, &q, variables); err != nil {
			return nil, fmt.Errorf("list inline comments: %w", err)
		}

		for _, thread := range q.Node.PullRequest.ReviewThreads.Nodes {
			threadID := fmt.Sprint(thread.ID)
			line := 0
			if thread.Line != nil {
				line = *thread.Line
			}

			for _, c := range thread.Comments.Nodes {
				comments = append(comments, &forge.InlineComment{
					ID: forge.InlineCommentThreadID(threadID),
					CommentID: &PRComment{
						GQLID: c.ID,
						URL:   c.URL,
					},
					Path:      thread.Path,
					Lines:     forge.InlineCommentLine(line),
					Body:      c.Body,
					Author:    c.Author.Login,
					Resolved:  thread.IsResolved,
					Outdated:  c.Outdated,
					CreatedAt: c.CreatedAt.Time,
				})
			}
		}

		pi := q.Node.PullRequest.ReviewThreads.PageInfo
		if !pi.HasNextPage {
			break
		}
		variables["after"] = pi.EndCursor
	}

	return comments, nil
}

// SubmitReview posts a batch of inline comments
// as a single review on a PR.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.ReviewRequest,
) error {
	pr := mustPR(id)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return err
	}

	// Map review event to GitHub's enum.
	var event githubv4.PullRequestReviewEvent
	switch req.Event {
	case forge.ReviewApprove:
		event = githubv4.PullRequestReviewEventApprove
	case forge.ReviewRequestChanges:
		event = githubv4.PullRequestReviewEventRequestChanges
	default:
		event = githubv4.PullRequestReviewEventComment
	}

	// Build thread inputs for inline comments.
	var threads []*githubv4.DraftPullRequestReviewThread
	for _, c := range req.Comments {
		side := githubv4.DiffSideRight
		if c.Side == forge.InlineCommentSideLeft {
			side = githubv4.DiffSideLeft
		}
		threads = append(threads,
			&githubv4.DraftPullRequestReviewThread{
				Path: new(
					githubv4.String(c.Path),
				),
				Line: new(
					githubv4.Int(c.Lines.StartLine),
				),
				Side: &side,
				Body: githubv4.String(c.Body),
			})
	}

	var m struct {
		AddPullRequestReview struct {
			PullRequestReview struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"pullRequestReview"`
		} `graphql:"addPullRequestReview(input: $input)"`
	}

	input := githubv4.AddPullRequestReviewInput{
		PullRequestID: gqlID,
		Event:         &event,
		Threads:       &threads,
	}
	if req.Body != "" {
		input.Body = new(githubv4.String(req.Body))
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}

	r.log.Debug("Submitted review",
		"pr", pr.Number,
		"comments", len(req.Comments),
	)
	return nil
}

// PostInlineComment posts a single inline comment
// on a PR by creating a review thread.
func (r *Repository) PostInlineComment(
	ctx context.Context,
	id forge.ChangeID,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	pr := mustPR(id)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return nil, err
	}

	// If replying to an existing thread, use addPullRequestReviewComment.
	if req.InReplyTo != "" {
		return r.replyToThread(ctx, req)
	}

	side := githubv4.DiffSideRight
	if req.Side == forge.InlineCommentSideLeft {
		side = githubv4.DiffSideLeft
	}

	var m struct {
		AddPullRequestReviewThread struct {
			Thread struct {
				ID       githubv4.ID `graphql:"id"`
				Comments struct {
					Nodes []struct {
						ID        githubv4.ID       `graphql:"id"`
						URL       string            `graphql:"url"`
						CreatedAt githubv4.DateTime `graphql:"createdAt"`
					} `graphql:"nodes"`
				} `graphql:"comments(first: 1)"`
			} `graphql:"thread"`
		} `graphql:"addPullRequestReviewThread(input: $input)"`
	}

	input := githubv4.AddPullRequestReviewThreadInput{
		PullRequestID: &gqlID,
		Path: new(
			githubv4.String(req.Path),
		),
		Line: new(
			githubv4.Int(req.Lines.StartLine),
		),
		Side: &side,
		Body: githubv4.String(req.Body),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return nil, fmt.Errorf("post inline comment: %w", err)
	}

	thread := m.AddPullRequestReviewThread.Thread
	threadID := fmt.Sprint(thread.ID)

	var commentID forge.ChangeCommentID
	var createdAt time.Time
	if len(thread.Comments.Nodes) > 0 {
		n := thread.Comments.Nodes[0]
		commentID = &PRComment{GQLID: n.ID, URL: n.URL}
		createdAt = n.CreatedAt.Time
	}

	r.log.Debug("Posted inline comment",
		"pr", pr.Number,
		"path", req.Path,
		"line", req.Lines.StartLine,
	)

	return &forge.InlineComment{
		ID:        forge.InlineCommentThreadID(threadID),
		CommentID: commentID,
		Path:      req.Path,
		Lines:     req.Lines,
		Body:      req.Body,
		CreatedAt: createdAt,
	}, nil
}

// replyToThread adds a reply to an existing review thread.
func (r *Repository) replyToThread(
	ctx context.Context,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	var m struct {
		AddPullRequestReviewThreadReply struct {
			Comment struct {
				ID        githubv4.ID       `graphql:"id"`
				URL       string            `graphql:"url"`
				CreatedAt githubv4.DateTime `graphql:"createdAt"`
			} `graphql:"comment"`
		} `graphql:"addPullRequestReviewThreadReply(input: $input)"`
	}

	input := githubv4.AddPullRequestReviewThreadReplyInput{
		PullRequestReviewThreadID: githubv4.ID(req.InReplyTo),
		Body:                      githubv4.String(req.Body),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return nil, fmt.Errorf("reply to thread: %w", err)
	}

	n := m.AddPullRequestReviewThreadReply.Comment
	return &forge.InlineComment{
		ID:        req.InReplyTo,
		CommentID: &PRComment{GQLID: n.ID, URL: n.URL},
		Path:      req.Path,
		Lines:     req.Lines,
		Body:      req.Body,
		CreatedAt: n.CreatedAt.Time,
	}, nil
}

// ResolveThread marks a review thread as resolved.
func (r *Repository) ResolveThread(
	ctx context.Context,
	id forge.InlineCommentThreadID,
) error {
	var m struct {
		ResolveReviewThread struct {
			Thread struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"thread"`
		} `graphql:"resolveReviewThread(input: $input)"`
	}

	input := githubv4.ResolveReviewThreadInput{
		ThreadID: githubv4.ID(id),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("resolve thread: %w", err)
	}

	r.log.Debug("Resolved thread", "id", id)
	return nil
}

// UnresolveThread marks a review thread as unresolved.
func (r *Repository) UnresolveThread(
	ctx context.Context,
	id forge.InlineCommentThreadID,
) error {
	var m struct {
		UnresolveReviewThread struct {
			Thread struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"thread"`
		} `graphql:"unresolveReviewThread(input: $input)"`
	}

	input := githubv4.UnresolveReviewThreadInput{
		ThreadID: githubv4.ID(id),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("unresolve thread: %w", err)
	}

	r.log.Debug("Unresolved thread", "id", id)
	return nil
}

// EditComment updates the body of an existing review comment.
func (r *Repository) EditComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	cid := mustPRComment(id)

	var m struct {
		UpdatePullRequestReviewComment struct {
			PullRequestReviewComment struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"pullRequestReviewComment"`
		} `graphql:"updatePullRequestReviewComment(input: $input)"`
	}

	input := githubv4.UpdatePullRequestReviewCommentInput{
		PullRequestReviewCommentID: cid.GQLID,
		Body:                       githubv4.String(body),
	}

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}

	r.log.Debug("Edited comment", "url", cid.URL)
	return nil
}
