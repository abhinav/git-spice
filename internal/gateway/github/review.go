package github

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"
)

// GitHub requires connection page sizes from 1 through 100.
// See https://docs.github.com/en/graphql/guides/using-pagination-in-the-graphql-api#about-pagination.
const reviewConnectionPageSize = 100

// ReviewAuthor identifies the GitHub user that authored a review or comment.
type ReviewAuthor struct {
	// Login is the user's GitHub login.
	Login string `json:"login"`
}

// PullRequestReviewComment is GitHub's wire representation of a review comment.
type PullRequestReviewComment struct {
	// ID is the comment's GraphQL node ID.
	ID ID `json:"id"`
	// URL is GitHub's browser URL for the comment.
	URL string `json:"url"`
	// Body is the comment text.
	Body string `json:"body"`
	// Author identifies the comment author.
	Author ReviewAuthor `json:"author"`

	// OriginalCommit identifies the original reviewed change head.
	OriginalCommit struct {
		OID string `json:"oid"`
	} `json:"originalCommit"`

	// CreatedAt is when GitHub created the comment.
	CreatedAt time.Time `json:"createdAt"`
}

// PullRequestReviewThread is GitHub's wire representation of a review thread.
type PullRequestReviewThread struct {
	// ID is the thread's GraphQL node ID.
	ID ID `json:"id"`
	// Path is relative to the repository root.
	Path string `json:"path"`
	// SubjectType identifies whether the thread targets the file or a line.
	SubjectType ReviewThreadSubjectType `json:"subjectType"`
	// DiffSide identifies the side containing Line.
	DiffSide DiffSide `json:"diffSide"`
	// StartDiffSide identifies the side containing StartLine.
	StartDiffSide DiffSide `json:"startDiffSide"`
	// Line is the current inclusive end line, when GitHub exposes it.
	Line *int `json:"line"`
	// StartLine is the current inclusive start line for a multiline thread.
	StartLine *int `json:"startLine"`
	// OriginalLine is the original end line retained for outdated threads.
	OriginalLine *int `json:"originalLine"`
	// OriginalStartLine is the original start line retained for outdated threads.
	OriginalStartLine *int `json:"originalStartLine"`
	// IsResolved reports whether the thread is resolved.
	IsResolved bool `json:"isResolved"`
	// IsOutdated reports whether the thread refers to an earlier diff.
	IsOutdated bool `json:"isOutdated"`
	// Comments contains every comment in chronological connection order.
	Comments []PullRequestReviewComment `json:"-"`
}

// PullRequestReviewThreads yields every review thread and every comment in it.
func (c *Gateway) PullRequestReviewThreads(ctx context.Context, id ID, opts *PaginationOptions) iter.Seq2[*PullRequestReviewThread, error] {
	return func(yield func(*PullRequestReviewThread, error) bool) {
		first, err := paginationItemsPerPage(opts, reviewConnectionPageSize)
		if err != nil {
			yield(nil, err)
			return
		}

		var after *string
		for pageNum := 1; ; pageNum++ {
			page, err := c.pullRequestReviewThreadListPage(ctx, id, first, after)
			if err != nil {
				yield(nil, fmt.Errorf("list review threads (page %d): %w", pageNum, err))
				return
			}
			for _, node := range page.Nodes {
				thread := &node.PullRequestReviewThread
				thread.Comments = append(thread.Comments, node.Comments.Nodes...)
				commentsAfter := node.Comments.PageInfo.EndCursor
				for commentsPage := 2; node.Comments.PageInfo.HasNextPage; commentsPage++ {
					comments, err := c.pullRequestReviewCommentsPage(ctx, thread.ID, reviewConnectionPageSize, commentsAfter)
					if err != nil {
						yield(nil, fmt.Errorf("list review thread %s comments (page %d): %w", thread.ID, commentsPage, err))
						return
					}
					thread.Comments = append(thread.Comments, comments.Nodes...)
					node.Comments = *comments
					commentsAfter = comments.PageInfo.EndCursor
				}
				if !yield(thread, nil) {
					return
				}
			}
			if !page.PageInfo.HasNextPage {
				return
			}
			after = &page.PageInfo.EndCursor
		}
	}
}

// PullRequestLatestOpinionatedReview is the latest effective review
// from one reviewer.
type PullRequestLatestOpinionatedReview struct {
	// Author identifies the reviewer.
	Author ReviewAuthor `json:"author"`
	// State is the submitted review outcome.
	State ReviewState `json:"state"`
	// Commit identifies the reviewed revision.
	Commit struct {
		OID string `json:"oid"`
	} `json:"commit"`
	// SubmittedAt is when GitHub submitted the review.
	SubmittedAt time.Time `json:"submittedAt"`
}

// PullRequestLatestOpinionatedReviews yields GitHub's latest opinionated review
// per user.
func (c *Gateway) PullRequestLatestOpinionatedReviews(ctx context.Context, id ID, opts *PaginationOptions) iter.Seq2[*PullRequestLatestOpinionatedReview, error] {
	return func(yield func(*PullRequestLatestOpinionatedReview, error) bool) {
		first, err := paginationItemsPerPage(opts, reviewConnectionPageSize)
		if err != nil {
			yield(nil, err)
			return
		}
		var after *string
		for pageNum := 1; ; pageNum++ {
			var result struct {
				Node struct {
					LatestOpinionatedReviews struct {
						PageInfo struct {
							EndCursor   string `json:"endCursor"`
							HasNextPage bool   `json:"hasNextPage"`
						} `json:"pageInfo"`
						Nodes []*PullRequestLatestOpinionatedReview `json:"nodes"`
					} `json:"latestOpinionatedReviews"`
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
				query(
					$after: ` + afterType + `,
					$first: Int!,
					$id: ID!,
				){
					node(id: $id){
						... on PullRequest{
							latestOpinionatedReviews(first: $first, after: $after){
								pageInfo{endCursor,hasNextPage},
								nodes{
									author{login},
									state,
									commit{oid},
									submittedAt
								}
							}
						}
					}
				}
			`)
			if err := c.execute(ctx, query, variables, &result); err != nil {
				yield(nil, fmt.Errorf("list latest opinionated reviews (page %d): %w", pageNum, err))
				return
			}
			for _, review := range result.Node.LatestOpinionatedReviews.Nodes {
				if !yield(review, nil) {
					return
				}
			}
			if !result.Node.LatestOpinionatedReviews.PageInfo.HasNextPage {
				return
			}
			after = &result.Node.LatestOpinionatedReviews.PageInfo.EndCursor
		}
	}
}

// AddPullRequestReviewInput starts a pending review for one pull request.
// See https://docs.github.com/en/graphql/reference/pulls#addpullrequestreviewinput.
type AddPullRequestReviewInput struct {
	// PullRequestID is the pull request's GraphQL node ID.
	PullRequestID ID `json:"pullRequestId"`
}

// AddedPullRequestReview identifies a newly created pending review.
type AddedPullRequestReview struct {
	// ID is the review's GraphQL node ID.
	ID ID `json:"id"`
}

// AddPullRequestReview starts an empty pending review.
func (c *Gateway) AddPullRequestReview(ctx context.Context, input *AddPullRequestReviewInput) (*AddedPullRequestReview, error) {
	var result struct {
		AddPullRequestReview struct {
			PullRequestReview *AddedPullRequestReview `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	mutation := compactGraphQL(`
		mutation($input: AddPullRequestReviewInput!){
			addPullRequestReview(input: $input){
				pullRequestReview{id}
			}
		}
	`)
	if err := c.mutate(ctx, mutation, input, &result); err != nil {
		return nil, err
	}
	return result.AddPullRequestReview.PullRequestReview, nil
}

// AddPullRequestReviewThreadInput adds a new pull request review thread.
// See https://docs.github.com/en/graphql/reference/pulls#addpullrequestreviewthreadinput.
type AddPullRequestReviewThreadInput struct {
	// PullRequestID posts the thread directly to this pull request.
	PullRequestID ID `json:"pullRequestId,omitzero"`

	// PullRequestReviewID attaches the thread to this pending review.
	PullRequestReviewID ID `json:"pullRequestReviewId,omitzero"`

	// Path is relative to the repository root.
	Path string `json:"path"`

	// SubjectType identifies a whole-file thread when set to FILE.
	SubjectType ReviewThreadSubjectType `json:"subjectType,omitzero"`

	// Line is the inclusive end line for a line thread.
	Line int `json:"line,omitzero"`

	// Side identifies the side containing Line for a line thread.
	Side DiffSide `json:"side,omitzero"`

	// StartLine is the inclusive start line for a multiline thread.
	StartLine *int `json:"startLine,omitzero"`

	// StartSide identifies the side containing StartLine.
	StartSide *DiffSide `json:"startSide,omitzero"`

	// Body is the initial comment text.
	Body string `json:"body"`
}

// AddedPullRequestReviewComment identifies a newly created review comment.
type AddedPullRequestReviewComment struct {
	// ID is the comment's GraphQL node ID.
	ID ID `json:"id"`
	// URL is GitHub's browser URL for the comment.
	URL string `json:"url"`
}

// AddedPullRequestReviewThread identifies a newly created thread and comment.
type AddedPullRequestReviewThread struct {
	// ID is the thread's GraphQL node ID.
	ID ID `json:"id"`
	// Comment identifies the thread's initial comment.
	Comment *AddedPullRequestReviewComment `json:"-"`
}

// AddPullRequestReviewThread adds a new pull request review thread.
func (c *Gateway) AddPullRequestReviewThread(ctx context.Context, input *AddPullRequestReviewThreadInput) (*AddedPullRequestReviewThread, error) {
	var result struct {
		AddPullRequestReviewThread struct {
			Thread *struct {
				ID       ID `json:"id"`
				Comments struct {
					Nodes []*AddedPullRequestReviewComment `json:"nodes"`
				} `json:"comments"`
			} `json:"thread"`
		} `json:"addPullRequestReviewThread"`
	}
	mutation := compactGraphQL(`
		mutation($input: AddPullRequestReviewThreadInput!){
			addPullRequestReviewThread(input: $input){
				thread{
					id,
					comments(first: 1){
						nodes{id,url}
					}
				}
			}
		}
	`)
	if err := c.mutate(ctx, mutation, input, &result); err != nil {
		return nil, err
	}
	thread := result.AddPullRequestReviewThread.Thread
	if thread == nil || len(thread.Comments.Nodes) != 1 || thread.Comments.Nodes[0] == nil {
		return nil, errors.New("add review thread: expected one created comment")
	}
	return &AddedPullRequestReviewThread{ID: thread.ID, Comment: thread.Comments.Nodes[0]}, nil
}

// AddPullRequestReviewThreadReplyInput appends a reply to a review thread.
// See https://docs.github.com/en/graphql/reference/pulls#addpullrequestreviewthreadreplyinput.
type AddPullRequestReviewThreadReplyInput struct {
	// PullRequestReviewThreadID is the target thread's GraphQL node ID.
	PullRequestReviewThreadID ID `json:"pullRequestReviewThreadId"`

	// PullRequestReviewID attaches the reply to this pending review when set.
	PullRequestReviewID ID `json:"pullRequestReviewId,omitzero"`

	// Body is the reply text.
	Body string `json:"body"`
}

// AddPullRequestReviewThreadReply appends a reply to an existing thread.
func (c *Gateway) AddPullRequestReviewThreadReply(ctx context.Context, input *AddPullRequestReviewThreadReplyInput) (*AddedPullRequestReviewComment, error) {
	var result struct {
		AddPullRequestReviewThreadReply struct {
			Comment *AddedPullRequestReviewComment `json:"comment"`
		} `json:"addPullRequestReviewThreadReply"`
	}
	mutation := compactGraphQL(`
		mutation($input: AddPullRequestReviewThreadReplyInput!){
			addPullRequestReviewThreadReply(input: $input){
				comment{id,url}
			}
		}
	`)
	if err := c.mutate(ctx, mutation, input, &result); err != nil {
		return nil, err
	}
	return result.AddPullRequestReviewThreadReply.Comment, nil
}

// SubmitPullRequestReviewInput publishes a pending review.
// See https://docs.github.com/en/graphql/reference/pulls#submitpullrequestreviewinput.
type SubmitPullRequestReviewInput struct {
	// PullRequestReviewID is the pending review's GraphQL node ID.
	PullRequestReviewID ID `json:"pullRequestReviewId"`
	// Event is the review outcome.
	Event ReviewEvent `json:"event"`
	// Body is the optional top-level review text.
	Body string `json:"body,omitzero"`
}

// SubmitPullRequestReview publishes a pending review.
func (c *Gateway) SubmitPullRequestReview(ctx context.Context, input *SubmitPullRequestReviewInput) error {
	mutation := compactGraphQL(`
		mutation($input: SubmitPullRequestReviewInput!){
			submitPullRequestReview(input: $input){
				pullRequestReview{id}
			}
		}
	`)
	return c.mutate(ctx, mutation, input, &struct{}{})
}

// UpdatePullRequestReviewComment replaces a review comment body.
func (c *Gateway) UpdatePullRequestReviewComment(ctx context.Context, id ID, body string) error {
	mutation := compactGraphQL(`
		mutation($input: UpdatePullRequestReviewCommentInput!){
			updatePullRequestReviewComment(input: $input){
				pullRequestReviewComment{id}
			}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ID   ID     `json:"pullRequestReviewCommentId"`
		Body string `json:"body"`
	}{id, body}, &struct{}{})
}

// ResolveReviewThread marks a review thread resolved.
func (c *Gateway) ResolveReviewThread(ctx context.Context, id ID) error {
	mutation := compactGraphQL(`
		mutation($input: ResolveReviewThreadInput!){
			resolveReviewThread(input: $input){thread{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ThreadID ID `json:"threadId"`
	}{id}, &struct{}{})
}

// UnresolveReviewThread marks a review thread unresolved.
func (c *Gateway) UnresolveReviewThread(ctx context.Context, id ID) error {
	mutation := compactGraphQL(`
		mutation($input: UnresolveReviewThreadInput!){
			unresolveReviewThread(input: $input){thread{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		ThreadID ID `json:"threadId"`
	}{id}, &struct{}{})
}

// reviewCommentConnection is one page of review comments.
type reviewCommentConnection struct {
	PageInfo struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
	Nodes []PullRequestReviewComment `json:"nodes"`
}

// reviewThreadNode combines a thread with its first comment page.
type reviewThreadNode struct {
	PullRequestReviewThread
	Comments reviewCommentConnection `json:"comments"`
}

// reviewThreadListConnection is one page of review threads.
type reviewThreadListConnection struct {
	PageInfo struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
	Nodes []*reviewThreadNode `json:"nodes"`
}

// pullRequestReviewThreadListPage loads one thread page and each thread's
// first comment page.
func (c *Gateway) pullRequestReviewThreadListPage(ctx context.Context, id ID, first int, after *string) (*reviewThreadListConnection, error) {
	var result struct {
		Node struct {
			ReviewThreads reviewThreadListConnection `json:"reviewThreads"`
		} `json:"node"`
	}
	variables := struct {
		After         *string `json:"after"`
		CommentsFirst int     `json:"commentsFirst"`
		First         int     `json:"first"`
		ID            ID      `json:"id"`
	}{after, reviewConnectionPageSize, first, id}
	afterType := "String"
	if after != nil {
		afterType = "String!"
	}
	query := compactGraphQL(`
		query(
			$after: ` + afterType + `,
			$commentsFirst: Int!,
			$first: Int!,
			$id: ID!,
		){
			node(id: $id){
				... on PullRequest{
					reviewThreads(first: $first, after: $after){
						pageInfo{endCursor,hasNextPage},
						nodes{
							id,
							path,
							subjectType,
							diffSide,
							startDiffSide,
							line,
							startLine,
							originalLine,
							originalStartLine,
							isResolved,
							isOutdated,
							comments(first: $commentsFirst){
								pageInfo{endCursor,hasNextPage},
								nodes{
									id,
									url,
									body,
									author{login},
									originalCommit{oid},
									createdAt
								}
							}
						}
					}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return &result.Node.ReviewThreads, nil
}

// pullRequestReviewCommentsPage loads one continuation page for a thread.
func (c *Gateway) pullRequestReviewCommentsPage(ctx context.Context, id ID, first int, after string) (*reviewCommentConnection, error) {
	var result struct {
		Node struct {
			Comments reviewCommentConnection `json:"comments"`
		} `json:"node"`
	}
	variables := struct {
		After string `json:"after"`
		First int    `json:"first"`
		ID    ID     `json:"id"`
	}{after, first, id}
	query := compactGraphQL(`
		query($after: String!, $first: Int!, $id: ID!){
			node(id: $id){
				... on PullRequestReviewThread{
					comments(first: $first, after: $after){
						pageInfo{endCursor,hasNextPage},
						nodes{
							id,
							url,
							body,
							author{login},
							createdAt
						}
					}
				}
			}
		}
	`)
	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return &result.Node.Comments, nil
}
