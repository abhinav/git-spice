package git

import (
	"context"
	"fmt"
)

// CheckoutFilesRequest specifies the parameters for checking out files.
type CheckoutFilesRequest struct {
	// Pathspecs are the paths, or path patterns, to checkout.
	Pathspecs []string // required

	// TreeIsh is the tree-ish to checkout files from.
	// If empty, files will be checked out from the index.
	TreeIsh string

	// Overlay, when true, does not remove files that exist in the index
	// but not in the tree-ish being checked out.
	// When false (default), such files are removed.
	Overlay bool
}

// CheckoutFiles checks out files from the specified tree-ish to the working directory.
// This wraps 'git checkout [<tree-ish>] -- [<pathspec>...]'.
func (w *Worktree) CheckoutFiles(ctx context.Context, req *CheckoutFilesRequest) error {
	args := []string{"checkout"}
	if req.Overlay {
		args = append(args, "--overlay")
	} else {
		args = append(args, "--no-overlay")
	}

	if req.TreeIsh != "" {
		args = append(args, req.TreeIsh)
	}

	args = append(args, "--")
	args = append(args, req.Pathspecs...)
	if err := w.gitCmd(ctx, args...).Run(w.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}
