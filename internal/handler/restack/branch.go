package restack

import (
	"cmp"
	"context"
)

// BranchOptions holds options for restacking a branch.
type BranchOptions struct {
	// SkipConflicts indicates that conflicting branches should be skipped.
	SkipConflicts bool `help:"Skip branches that cannot be rebased due to conflicts"`
}

// RestackBranch restacks the given branch onto its base.
func (h *Handler) RestackBranch(ctx context.Context, branch string, opts *BranchOptions) error {
	opts = cmp.Or(opts, &BranchOptions{})
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		ContinueCommand: []string{"branch", "restack"},
		SkipConflicts:   opts.SkipConflicts,
	})
	return err
}
