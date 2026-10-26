package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// Thread is one pull request discussion returned by Azure DevOps.
type Thread struct {
	// ID identifies the thread within its pull request.
	ID int

	// Deleted reports whether Azure DevOps marked the thread as deleted.
	Deleted bool

	// Resolved reports whether the thread has a terminal discussion status.
	// Fixed, won't-fix, closed, and by-design threads are resolved.
	Resolved bool

	// Comments contains the thread's comments in Azure DevOps response order.
	Comments []Comment
}

// Comment is one comment in an Azure DevOps pull request thread.
type Comment struct {
	// ID identifies the comment within its thread.
	ID int

	// Body is the comment text.
	Body string

	// AuthorID is the Azure DevOps identity ID of the comment author.
	AuthorID string

	// Deleted reports whether Azure DevOps marked the comment as deleted.
	Deleted bool

	// System reports whether Azure DevOps generated the comment.
	System bool
}

// AddComment creates a pull request comment in a new closed thread.
// The closed status prevents the metadata comment from creating an unresolved
// discussion that can block pull request completion.
// It returns the IDs needed to update or delete the comment later.
func (g *Gateway) AddComment(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	body string,
) (threadID int, commentID int, err error) {
	commentType := git.CommentTypeValues.Text
	threadStatus := git.CommentThreadStatusValues.Closed
	thread, err := g.gitClient.CreateThread(ctx, git.CreateThreadArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &pullRequest,
		CommentThread: &git.GitPullRequestCommentThread{
			Status: &threadStatus,
			Comments: &[]git.Comment{{
				Content:     &body,
				CommentType: &commentType,
			}},
		},
	})
	if err != nil {
		return 0, 0, normalizeError(err)
	}
	if thread.Id != nil {
		threadID = *thread.Id
	}
	commentID = 1
	if thread.Comments != nil && len(*thread.Comments) > 0 &&
		(*thread.Comments)[0].Id != nil {
		commentID = *(*thread.Comments)[0].Id
	}
	return threadID, commentID, nil
}

// CommentExists reports whether a pull request comment exists and is not
// deleted.
// It returns [ErrNotFound] when Azure DevOps cannot find the thread or comment.
func (g *Gateway) CommentExists(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	thread int,
	comment int,
) (bool, error) {
	result, err := g.gitClient.GetComment(ctx, git.GetCommentArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &pullRequest,
		ThreadId:      &thread,
		CommentId:     &comment,
	})
	if err != nil {
		return false, normalizeError(err)
	}
	return result.IsDeleted == nil || !*result.IsDeleted, nil
}

// UpdateComment replaces a pull request comment body.
// It returns [ErrNotFound] when Azure DevOps cannot find the thread or comment.
func (g *Gateway) UpdateComment(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	thread int,
	comment int,
	body string,
) error {
	_, err := g.gitClient.UpdateComment(ctx, git.UpdateCommentArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &pullRequest,
		ThreadId:      &thread,
		CommentId:     &comment,
		Comment:       &git.Comment{Content: &body},
	})
	return normalizeError(err)
}

// DeleteComment deletes a pull request comment.
// It returns [ErrNotFound] when Azure DevOps cannot find the thread or comment.
func (g *Gateway) DeleteComment(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	thread int,
	comment int,
) error {
	return normalizeError(g.gitClient.DeleteComment(ctx, git.DeleteCommentArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &pullRequest,
		ThreadId:      &thread,
		CommentId:     &comment,
	}))
}

// Threads returns comment threads in Azure DevOps response order.
// Deleted threads and comments remain in the result with their Deleted fields
// set so callers can apply their own filtering policy.
func (g *Gateway) Threads(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
) ([]Thread, error) {
	threads, err := g.gitClient.GetThreads(ctx, git.GetThreadsArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &pullRequest,
	})
	if err != nil || threads == nil {
		return nil, normalizeError(err)
	}

	result := make([]Thread, 0, len(*threads))
	for _, thread := range *threads {
		converted := Thread{}
		if thread.Id != nil {
			converted.ID = *thread.Id
		}
		converted.Deleted = thread.IsDeleted != nil && *thread.IsDeleted
		if thread.Status != nil {
			converted.Resolved = isResolvedThreadStatus(*thread.Status)
		}
		if thread.Comments != nil {
			converted.Comments = make([]Comment, 0, len(*thread.Comments))
			for _, comment := range *thread.Comments {
				converted.Comments = append(converted.Comments, commentFromSDK(&comment))
			}
		}
		result = append(result, converted)
	}
	return result, nil
}

func commentFromSDK(comment *git.Comment) Comment {
	result := Comment{}
	if comment.Id != nil {
		result.ID = *comment.Id
	}
	if comment.Content != nil {
		result.Body = *comment.Content
	}
	if comment.Author != nil && comment.Author.Id != nil {
		result.AuthorID = *comment.Author.Id
	}
	result.Deleted = comment.IsDeleted != nil && *comment.IsDeleted
	result.System = comment.CommentType != nil &&
		*comment.CommentType == git.CommentTypeValues.System
	return result
}

func isResolvedThreadStatus(status git.CommentThreadStatus) bool {
	switch status {
	case git.CommentThreadStatusValues.Fixed,
		git.CommentThreadStatusValues.WontFix,
		git.CommentThreadStatusValues.Closed,
		git.CommentThreadStatusValues.ByDesign:
		return true
	default:
		return false
	}
}
