// Package spice intends to provide the core functionality of the tool.
package spice

import (
	"context"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -destination=mock_service_test.go -package=spice . GitRepository,GitWorktree,Store

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

	// LocalBranches returns an iterator over local branches
	LocalBranches(ctx context.Context, opts *git.LocalBranchesOptions) iter.Seq2[git.LocalBranch, error]

	// RemoteDefaultBranch reports the default branch of the given remote.
	RemoteDefaultBranch(ctx context.Context, remote string) (string, error)

	// ListRemotes returns the names of all known remotes.
	ListRemotes(ctx context.Context) ([]string, error)
	RemoteURL(ctx context.Context, remote string) (string, error)

	// ListRemoteRefs returns an iterator over references in a remote
	// Git repository that match the given options.
	ListRemoteRefs(
		ctx context.Context, remote string, opts *git.ListRemoteRefsOptions,
	) iter.Seq2[git.RemoteRef, error]

	RenameBranch(context.Context, git.RenameBranchRequest) error
	DeleteBranch(context.Context, string, git.BranchDeleteOptions) error
	HashAt(context.Context, string, string) (git.Hash, error)
}

// GitWorktree provides access to a Git worktree owned by a repository.
type GitWorktree interface {
	// CurrentBranch returns the name of the current branch.
	CurrentBranch(ctx context.Context) (string, error)
	Rebase(context.Context, git.RebaseRequest) error
}

var (
	_ GitRepository = (*git.Repository)(nil)
	_ GitWorktree   = (*git.Worktree)(nil)
)

// Store provides storage for gs.
// It is a subset of the functionality provided by the state.Store type.
type Store interface {
	// Trunk returns the name of the trunk branch.
	Trunk() string
	Remote() (string, error)

	// LookupBranch returns the branch state for the given branch,
	// or [state.ErrNotExist] if the branch does not exist.
	LookupBranch(ctx context.Context, name string) (*state.LookupResponse, error)

	// BeginBranchTx begins a transaction to modify state
	// for zero or more branches
	BeginBranchTx() *state.BranchTx

	// ListBranches returns a list of all tracked branch names.
	// This list never includes the trunk branch.
	ListBranches(ctx context.Context) iter.Seq2[string, error]

	AppendContinuations(context.Context, string, ...state.Continuation) error
	TakeContinuations(context.Context, string) ([]state.Continuation, error)

	LoadCachedTemplates(context.Context) (string, []*state.CachedTemplate, error)
	CacheTemplates(context.Context, string, []*state.CachedTemplate) error
}

var _ Store = (*state.Store)(nil)

// Service provides the core functionality of the tool.
// It combines together lower level pieces like access to the git repository
// and the spice state.
type Service struct {
	repo   GitRepository // required
	wt     GitWorktree   // required
	store  Store         // required
	log    *silog.Logger
	forges *forge.Registry
}

// NewService builds a new service operating on the given repository and store.
func NewService(
	repo GitRepository,
	wt GitWorktree,
	store Store,
	forges *forge.Registry,
	log *silog.Logger,
) *Service {
	return newService(repo, wt, store, forges, log)
}

func newService(
	repo GitRepository,
	wt GitWorktree,
	store Store,
	forges *forge.Registry,
	log *silog.Logger,
) *Service {
	return &Service{
		repo:   repo,
		wt:     wt,
		store:  store,
		log:    log,
		forges: forges,
	}
}

// Trunk reports the name of the trunk branch.
func (s *Service) Trunk() string {
	return s.store.Trunk()
}

// BranchGraph builds a full view of the graph of branches in the repository.
func (s *Service) BranchGraph(ctx context.Context, opts *BranchGraphOptions) (*BranchGraph, error) {
	// TODO: cache branch graph based on hash of store contents
	return NewBranchGraph(ctx, s, opts)
}

// LookupWorktrees returns a map of branch names to worktree paths.
func (s *Service) LookupWorktrees(ctx context.Context, branches []string) (map[string]string, error) {
	want := make(map[string]struct{}, len(branches))
	for _, b := range branches {
		want[b] = struct{}{}
	}

	worktrees := make(map[string]string, len(branches))
	for b, err := range s.repo.LocalBranches(ctx, nil) {
		if err != nil {
			return nil, err
		}

		if b.Worktree == "" {
			continue
		}

		if _, ok := want[b.Name]; !ok {
			// Not a branch we care about.
			continue
		}

		worktrees[b.Name] = b.Worktree
	}

	return worktrees, nil
}
