// Package restack implements business logic for high-level restack operations.
package restack

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

//go:generate mockgen -package restack -destination mocks_test.go . GitWorktree,Service

// GitWorktree is a subet of the git.Worktree interface.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	Checkout(ctx context.Context, branch string) error
}

// Store is a subset of the state.Store interface.
type Store interface {
	Trunk() string
}

// Service is a subset of the spice.Service interface.
type Service interface {
	LoadBranches(ctx context.Context) ([]spice.LoadBranchItem, error)
	ListUpstack(ctx context.Context, branch string) ([]string, error)
	ListStack(ctx context.Context, branch string) ([]string, error)
	Restack(ctx context.Context, name string) (*spice.RestackResponse, error)
	RebaseRescue(ctx context.Context, req spice.RebaseRescueRequest) error
}

// Handler implements various restack operations.
type Handler struct {
	Log      *silog.Logger // required
	Worktree GitWorktree   // required
	Store    Store         // required
	Service  Service       // required
}
