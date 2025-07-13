// Package restack implements business logic for high-level restack operations.
package restack

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

// GitWorktree is a subet of the git.Worktree interface.
type GitWorktree interface {
	Checkout(ctx context.Context, branch string) error
}

// Store is a subset of the state.Store interface.
type Store interface {
	Trunk() string
}

// Service is a subset of the spice.Service interface.
type Service interface {
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
