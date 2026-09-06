// Package review coordinates review-comment command workflows.
package review

import (
	"context"
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
	Log    *silog.Logger // required
	Store  Store         // required
	Editor CommentEditor // required
}

// ThreadHandler coordinates review-thread resolution changes.
type ThreadHandler struct {
	Log        *silog.Logger              // required
	Service    Service                    // required
	Repository forge.ReviewRepository     // required
	Resolver   forge.ReviewThreadResolver // required
}

// Worktree provides the Git operations used by review workflows.
type Worktree interface {
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
