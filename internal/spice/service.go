// Package spice intends to provide the core functionality of the tool.
package spice

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// GitRepository provides read/write access to the conents of a git repository.
// It is a subset of the functionality provied by the git.Repository type.
type GitRepository interface {
	// MergeBase reports the merge base of the two given commits.
	// This is a commit that is an ancestor of both commits.
	MergeBase(ctx context.Context, a, b string) (git.Hash, error)

	// IsAncestor reports whether commit a is an ancestor of commit b.
	IsAncestor(ctx context.Context, a, b git.Hash) bool

	// ForkPoint reports the git hash at which branch b
	// forked from branch a.
	ForkPoint(ctx context.Context, a, b string) (git.Hash, error)

	// PeelToCommit returns the commit hash for the given commit-ish.
	PeelToCommit(ctx context.Context, ref string) (git.Hash, error)

	// CurrentBranch returns the name of the current branch.
	CurrentBranch(ctx context.Context) (string, error)

	// LocalBranches returns a list of all local branches.
	LocalBranches(ctx context.Context) ([]git.LocalBranch, error)

	// RemoteDefaultBranch reports the default branch of the given remote.
	RemoteDefaultBranch(ctx context.Context, remote string) (string, error)

	// ListRemotes returns the names of all known remotes.
	ListRemotes(ctx context.Context) ([]string, error)

	Rebase(context.Context, git.RebaseRequest) error
	RenameBranch(context.Context, git.RenameBranchRequest) error
	DeleteBranch(context.Context, string, git.BranchDeleteOptions) error
}

var _ GitRepository = (*git.Repository)(nil)

// Store provides storage for gs.
// It is a subset of the functionality provided by the state.Store type.
type Store interface {
	// Trunk returns the name of the trunk branch.
	Trunk() string

	// TODO: suffix "Branch(es)" to Lookup, Update, List

	// Lookup returns the branch state for the given branch,
	// or [state.ErrNotExist] if the branch does not exist.
	Lookup(ctx context.Context, name string) (*state.LookupResponse, error)

	// Update adds, updates, or removes state information
	// for zero or more branches.
	Update(ctx context.Context, req *state.UpdateRequest) error

	// List returns a list of all tracked branch names.
	// This list never includes the trunk branch.
	List(ctx context.Context) ([]string, error)

	AppendContinuations(context.Context, string, ...state.Continuation) error
	TakeContinuations(context.Context, string) ([]state.Continuation, error)
}

var _ Store = (*state.Store)(nil)

// Service provides the core functionality of the tool.
// It combines together lower level pieces like access to the git repository
// and the spice state.
type Service struct {
	repo  GitRepository
	store Store
	log   *log.Logger
}

// NewService builds a new service operating on the given repository and store.
func NewService(repo GitRepository, store Store, log *log.Logger) *Service {
	return &Service{
		repo:  repo,
		store: store,
		log:   log,
	}
}
