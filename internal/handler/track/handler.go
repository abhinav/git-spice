// Package track implements the Handler for various 'track' commands.
package track

import (
	"context"
	"iter"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination mocks_test.go -package track -typed . GitRepository,Service

// GitRepository provides read access to the Git repository's state.
type GitRepository interface {
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)
	ListCommits(ctx context.Context, commits git.CommitRange) iter.Seq2[git.Hash, error]
	LocalBranches(ctx context.Context, opts *git.LocalBranchesOptions) iter.Seq2[git.LocalBranch, error]
}

var _ GitRepository = (*git.Repository)(nil)

// Store is the storage for git-spice's state.
type Store interface {
	// Trunk reports the name of the trunk branch.
	Trunk() string

	// ListBranches lists all tracked branches.
	ListBranches(ctx context.Context) iter.Seq2[string, error]

	// BeginBranchTx begins a transaction for modifying branch state.
	BeginBranchTx() *state.BranchTx
}

var _ Store = (*state.Store)(nil)

// Service is a subset of spice.Service
// that is required by the Handler.
type Service interface {
	// VerifyRestacked verifies if the branch is restacked
	// and returns an error if it is not.
	VerifyRestacked(ctx context.Context, name string) error
}

var _ Service = (*spice.Service)(nil)

// Handler implements the business logic for the 'track' commands.
type Handler struct {
	Log        *silog.Logger // required
	View       ui.View       // required
	Repository GitRepository // required
	Store      Store         // required
	Service    Service       // required
}
