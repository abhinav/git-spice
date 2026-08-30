// Package review coordinates review-comment command workflows.
package review

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// Anchor identifies a file or inclusive line range for a review comment.
type Anchor = review.Anchor

// Draft is a local review comment waiting to be published.
type Draft = review.Draft

// DraftID identifies a local review draft within one branch.
type DraftID = review.DraftID

// Handler coordinates local and remote review-comment workflows.
type Handler struct {
	Log        *silog.Logger          // required
	Worktree   Worktree               // required
	Service    Service                // required
	Store      Store                  // required
	Repository forge.ReviewRepository // required
	Editor     CommentEditor          // required
}

// DraftHandler coordinates workflows that only access local drafts.
type DraftHandler struct {
	Log      *silog.Logger // required
	Worktree Worktree      // required
	Store    Store         // required
	Editor   CommentEditor // required
}

// ThreadHandler coordinates review-thread resolution changes.
type ThreadHandler struct {
	Log        *silog.Logger              // required
	Worktree   Worktree                   // required
	Service    Service                    // required
	Repository forge.ReviewRepository     // required
	Resolver   forge.ReviewThreadResolver // required
}

// Worktree provides the Git operations used by review workflows.
type Worktree interface {
	CurrentBranch(context.Context) (string, error)
	OpenBranchDiff(context.Context, string, string) (io.ReadCloser, error)
}

var _ Worktree = (*git.Worktree)(nil)

// Service provides tracked-branch information used by review workflows.
type Service interface {
	LookupBranch(context.Context, string) (*spice.LookupBranchResponse, error)
}

var _ Service = (*spice.Service)(nil)

// Store persists branch-local review drafts.
type Store interface {
	AddReviewDraft(context.Context, string, review.Draft) (review.Draft, error)
	LoadReviewDrafts(context.Context, string) ([]review.Draft, error)
	UpdateReviewDraftBody(context.Context, string, review.DraftID, string) error
	ClearReviewDrafts(context.Context, string) error
}

var _ Store = (*state.Store)(nil)

// CommentEditor opens a comment body for editing.
type CommentEditor func(
	ctx context.Context,
	initial string,
) (string, error)

//go:generate mockgen -destination=mocks_test.go -package=review -typed . Worktree,Service,Store
//go:generate mockgen -destination=forge_mocks_test.go -package=review -typed go.abhg.dev/gs/internal/forge ReviewRepository,ReviewThreadResolver

func resolveBranch(
	ctx context.Context,
	worktree Worktree,
	branch string,
) (string, error) {
	if branch != "" {
		return branch, nil
	}

	branch, err := worktree.CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return branch, nil
}

func lookupReviewChange(
	ctx context.Context,
	service Service,
	branch string,
) (*spice.LookupBranchResponse, error) {
	change, err := lookupBranch(ctx, service, branch)
	if err != nil {
		return nil, err
	}
	if change.Change == nil {
		return nil, fmt.Errorf(
			"no change request for %s; "+
				"submit the branch first with "+
				"'gs branch submit'",
			branch,
		)
	}
	return change, nil
}

func lookupBranch(
	ctx context.Context,
	service Service,
	branch string,
) (*spice.LookupBranchResponse, error) {
	change, err := service.LookupBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil, fmt.Errorf("branch not tracked: %s", branch)
		}
		return nil, fmt.Errorf("get branch: %w", err)
	}
	return change, nil
}

func findReviewThreadID(
	ctx context.Context,
	repository forge.ReviewRepository,
	changeID forge.ChangeID,
	want string,
) (forge.ReviewThreadID, error) {
	// ReviewThreadID is intentionally opaque.
	// Recover the provider-owned value whose String form the CLI accepted.
	for thread, err := range repository.ListReviewThreads(ctx, changeID) {
		if err != nil {
			return nil, fmt.Errorf("list review threads: %w", err)
		}
		if thread.ID.String() == want {
			return thread.ID, nil
		}
	}
	return nil, fmt.Errorf("review thread %q not found", want)
}
