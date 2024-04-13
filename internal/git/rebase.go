package git

import (
	"context"
	"fmt"
	"os"
)

// RebaseRequest is a request to rebase a branch.
type RebaseRequest struct {
	// Branch is the branch to rebase.
	Branch string

	// Upstream is the upstream commitish
	// from which the current branch started.
	//
	// Commits between Upstream and Branch will be rebased.
	Upstream string

	// Onto is the new base commit to rebase onto.
	// If unspecified, defaults to Upstream.
	Onto string

	// Autostash is true if the rebase should automatically stash
	// dirty changes before starting the rebase operation,
	// and re-apply them after the rebase is complete.
	Autostash bool

	// Quiet reduces the output of the rebase operation.
	Quiet bool

	// Interactive is true if the rebase should present the user
	// with a list of rebase instructions to edit
	// before starting the rebase operation.
	Interactive bool
}

// Rebase runs a git rebase operation with the specified parameters.
func (r *Repository) Rebase(ctx context.Context, req RebaseRequest) error {
	args := []string{"rebase"}
	if req.Interactive {
		args = append(args, "--interactive")
	}
	if req.Onto != "" {
		args = append(args, "--onto", req.Onto)
	}
	if req.Autostash {
		args = append(args, "--autostash")
	}
	if req.Quiet {
		args = append(args, "--quiet")
	}
	if req.Upstream != "" {
		args = append(args, req.Upstream)
	}
	if req.Branch != "" {
		args = append(args, req.Branch)
	}

	err := r.gitCmd(ctx, args...).
		Stdin(os.Stdin).
		Stdout(os.Stdout).
		Stderr(os.Stderr).
		Run(r.exec)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	return nil
}
