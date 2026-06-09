package restack

import (
	"cmp"
	"context"
)

// DownstackOptions holds options for restacking a downstack.
type DownstackOptions struct {
	// SkipConflicts indicates that conflicting branches should be skipped.
	SkipConflicts bool `help:"Skip branches that cannot be rebased due to conflicts"`
}

// RestackDownstack restacks the downstack of the given branch.
// This includes the branch itself.
func (h *Handler) RestackDownstack(ctx context.Context, branch string, opts *DownstackOptions) error {
	opts = cmp.Or(opts, &DownstackOptions{})
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		Scope:           ScopeDownstack,
		ContinueCommand: []string{"downstack", "restack"},
		SkipConflicts:   opts.SkipConflicts,
	})
	return err
}
