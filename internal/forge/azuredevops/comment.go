package azuredevops

import (
	"context"
	"fmt"
	"iter"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// PostChangeComment creates a new comment thread on a pull request.
// Returns the ID of the first comment in the thread.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	body string,
) (forge.ChangeCommentID, error) {
	prID := mustPR(id).Number

	// Create a new thread with one comment.
	// The thread is created with "closed" status
	// so it doesn't block merge in Azure DevOps.
	commentType := git.CommentTypeValues.Text
	threadStatus := git.CommentThreadStatusValues.Closed
	thread, err := r.client.gitClient.CreateThread(ctx, git.CreateThreadArgs{
		Project:       new(r.project()),
		RepositoryId:  new(r.repositoryID()),
		PullRequestId: &prID,
		CommentThread: &git.GitPullRequestCommentThread{
			Status: &threadStatus,
			Comments: &[]git.Comment{
				{
					Content:     &body,
					CommentType: &commentType,
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create comment thread: %w", err)
	}

	threadID := 0
	if thread.Id != nil {
		threadID = *thread.Id
	}

	// Get the first comment ID.
	commentID := 1 // Default to 1 as the first comment in a thread.
	if thread.Comments != nil && len(*thread.Comments) > 0 {
		firstComment := (*thread.Comments)[0]
		if firstComment.Id != nil {
			commentID = *firstComment.Id
		}
	}

	return &PRComment{
		PRID:      prID,
		ThreadID:  threadID,
		CommentID: commentID,
	}, nil
}

// UpdateChangeComment updates an existing comment in a thread.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	comment := mustPRComment(id)

	existing, err := r.client.gitClient.GetComment(ctx, git.GetCommentArgs{
		Project:       new(r.project()),
		RepositoryId:  new(r.repositoryID()),
		PullRequestId: &comment.PRID,
		ThreadId:      &comment.ThreadID,
		CommentId:     &comment.CommentID,
	})
	if err != nil {
		if isAzureStatus(err, http.StatusNotFound) {
			return fmt.Errorf("get comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("get comment: %w", err)
	}
	if existing.IsDeleted != nil && *existing.IsDeleted {
		return fmt.Errorf("get comment: %w", forge.ErrNotFound)
	}

	_, err = r.client.gitClient.UpdateComment(ctx, git.UpdateCommentArgs{
		Project:       new(r.project()),
		RepositoryId:  new(r.repositoryID()),
		PullRequestId: &comment.PRID,
		ThreadId:      &comment.ThreadID,
		CommentId:     &comment.CommentID,
		Comment: &git.Comment{
			Content: &body,
		},
	})
	if err != nil {
		if isAzureStatus(err, http.StatusNotFound) {
			return fmt.Errorf("update comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}

	r.log.Debug("Updated comment",
		"pr", comment.PRID,
		"thread", comment.ThreadID,
		"comment", comment.CommentID)
	return nil
}

// DeleteChangeComment deletes an existing comment from a pull request thread.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	comment := mustPRComment(id)

	if err := r.client.gitClient.DeleteComment(ctx, git.DeleteCommentArgs{
		Project:       new(r.project()),
		RepositoryId:  new(r.repositoryID()),
		PullRequestId: &comment.PRID,
		ThreadId:      &comment.ThreadID,
		CommentId:     &comment.CommentID,
	}); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	r.log.Debug("Deleted comment",
		"pr", comment.PRID,
		"thread", comment.ThreadID,
		"comment", comment.CommentID)
	return nil
}

// ListChangeComments lists comments on a pull request.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		prID := mustPR(id).Number

		threads, err := r.client.gitClient.GetThreads(ctx, git.GetThreadsArgs{
			Project:       new(r.project()),
			RepositoryId:  new(r.repositoryID()),
			PullRequestId: &prID,
		})
		if err != nil {
			yield(nil, fmt.Errorf("get threads: %w", err))
			return
		}

		if threads == nil {
			return
		}

		for _, thread := range *threads {
			if thread.Comments == nil ||
				(thread.IsDeleted != nil && *thread.IsDeleted) {
				continue
			}

			threadID := 0
			if thread.Id != nil {
				threadID = *thread.Id
			}

			for _, comment := range *thread.Comments {
				if comment.IsDeleted != nil && *comment.IsDeleted {
					continue
				}

				body := ""
				if comment.Content != nil {
					body = *comment.Content
				}

				// Skip system comments.
				if comment.CommentType != nil &&
					*comment.CommentType == git.CommentTypeValues.System {
					continue
				}

				commentID := 0
				if comment.Id != nil {
					commentID = *comment.Id
				}

				// Apply filters if specified.
				if opts != nil {
					// Filter by body matches.
					if len(opts.BodyMatchesAll) > 0 {
						allMatch := true
						for _, re := range opts.BodyMatchesAll {
							if !re.MatchString(body) {
								allMatch = false
								break
							}
						}
						if !allMatch {
							continue
						}
					}

					// Filter by can update.
					// For Azure DevOps, we'd need to check if the current user
					// is the author. For now, skip this filter.
					// TODO: Implement CanUpdate filter.
				}

				item := &forge.ListChangeCommentItem{
					ID: &PRComment{
						PRID:      prID,
						ThreadID:  threadID,
						CommentID: commentID,
					},
					Body: body,
				}

				if !yield(item, nil) {
					return
				}
			}
		}
	}
}
