package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

type reviewCmd struct {
	Comment reviewCommentCmd `cmd:"" help:"Draft or post a review comment"`
	Reply   reviewReplyCmd   `cmd:"" help:"Draft or post a reply to a review thread"`
	Publish reviewPublishCmd `cmd:"" help:"Publish draft comments as a review"`
	List    reviewListCmd    `cmd:"" aliases:"ls" help:"List review comments"`
	Edit    reviewEditCmd    `cmd:"" help:"Edit a draft comment"`
	Resolve reviewResolveCmd `cmd:"" help:"Resolve a review thread"`
	Reopen  reviewReopenCmd  `cmd:"" help:"Reopen a resolved review thread"`
}

func (*reviewCmd) AfterApply(kctx *kong.Context) error {
	return errors.Join(
		kctx.BindToProvider(func(
			log *silog.Logger,
			wt *git.Worktree,
			svc *spice.Service,
			store *state.Store,
			gitRepo *git.Repository,
			remoteRepo *remoteRepository,
		) (ReviewHandler, error) {
			repository, err := requireReviewRepository(remoteRepo.Repository)
			if err != nil {
				return nil, err
			}
			return &review.Handler{
				Log:        log,
				Worktree:   wt,
				Service:    svc,
				Store:      store,
				Repository: repository,
				Editor:     newReviewCommentEditor(gitRepo),
			}, nil
		}),
		kctx.BindToProvider(func(
			log *silog.Logger,
			store *state.Store,
			gitRepo *git.Repository,
		) (ReviewDraftHandler, error) {
			return &review.DraftHandler{
				Log:    log,
				Store:  store,
				Editor: newReviewCommentEditor(gitRepo),
			}, nil
		}),
		kctx.BindToProvider(func(
			log *silog.Logger,
			svc *spice.Service,
			remoteRepo *remoteRepository,
		) (ReviewThreadHandler, error) {
			repository, err := requireReviewRepository(remoteRepo.Repository)
			if err != nil {
				return nil, err
			}
			resolver, ok := remoteRepo.Repository.(forge.ReviewThreadResolver)
			if !ok {
				return nil, fmt.Errorf(
					"forge %q does not support review thread resolution: %w",
					remoteRepo.Repository.Forge().ID(),
					forge.ErrUnsupported,
				)
			}
			return &review.ThreadHandler{
				Log:        log,
				Service:    svc,
				Repository: repository,
				Resolver:   resolver,
			}, nil
		}),
	)
}

// ReviewHandler runs workflows that access review comments on a forge.
type ReviewHandler interface {
	PostComment(context.Context, *review.CommentRequest) error
	PostReply(context.Context, *review.ReplyRequest) error
	PublishDrafts(context.Context, *review.PublishDraftsRequest) error
	LoadReviewData(context.Context, *review.LoadRequest) (*review.LoadResult, error)
}

// ReviewDraftHandler runs workflows that only access local review drafts.
type ReviewDraftHandler interface {
	SaveCommentDraft(context.Context, *review.CommentRequest) error
	SaveReplyDraft(context.Context, *review.ReplyRequest) error
	ReplaceDraftBody(context.Context, *review.ReplaceDraftBodyRequest) error
}

// ReviewThreadHandler changes the resolution state of review threads.
type ReviewThreadHandler interface {
	SetThreadResolution(
		context.Context,
		*review.SetThreadResolutionRequest,
	) error
}

func requireReviewRepository(
	repository forge.Repository,
) (forge.ReviewRepository, error) {
	reviewRepository, ok := repository.(forge.ReviewRepository)
	if !ok {
		return nil, fmt.Errorf(
			"forge %q does not support review comments: %w",
			repository.Forge().ID(),
			forge.ErrUnsupported,
		)
	}
	return reviewRepository, nil
}

func newReviewCommentEditor(repo *git.Repository) review.CommentEditor {
	return func(ctx context.Context, initial string) (string, error) {
		return editReviewCommentBody(ctx, repo, initial)
	}
}
