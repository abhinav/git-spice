package restack

import (
	"cmp"
	"context"
)

// UpstackOptions holds options for restacking the upstack of a branch.
type UpstackOptions struct {
	// SkipStart indicates that the starting branch should not be restacked.
	SkipStart bool `help:"Do not restack the starting branch"`

	// SkipConflicts indicates that conflicting branches should be skipped.
	SkipConflicts bool `help:"Skip branches that cannot be rebased due to conflicts"`
}

// RestackUpstack restacks the upstack of the given branch,
// including the branch itself, unless SkipStart is set.
func (h *Handler) RestackUpstack(ctx context.Context, branch string, opts *UpstackOptions) error {
	opts = cmp.Or(opts, &UpstackOptions{})
	req := &Request{
		Branch:          branch,
		Scope:           ScopeUpstack,
		ContinueCommand: []string{"upstack", "restack"},
		SkipConflicts:   opts.SkipConflicts,
	}
	if opts.SkipStart {
		req.Scope = ScopeUpstackExclusive
		req.ContinueCommand = []string{"upstack", "restack", "--skip-start"}
	}
	_, err := h.Restack(ctx, req)
	return err
}
