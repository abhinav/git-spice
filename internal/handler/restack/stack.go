package restack

import (
	"cmp"
	"context"
)

// StackOptions holds options for restacking a stack.
type StackOptions struct {
	// SkipConflicts indicates that conflicting branches should be skipped.
	SkipConflicts bool `help:"Skip branches that cannot be rebased due to conflicts"`
}

// RestackStack restacks the stack of the given branch.
// This includes all upstack and downtrack branches,
// as well as the branch itself.
func (h *Handler) RestackStack(ctx context.Context, branch string, opts *StackOptions) error {
	opts = cmp.Or(opts, &StackOptions{})
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		Scope:           ScopeStack,
		ContinueCommand: []string{"stack", "restack"},
		SkipConflicts:   opts.SkipConflicts,
	})
	return err
}
