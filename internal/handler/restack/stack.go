package restack

import "context"

// RestackStack restacks the stack of the given branch.
// This includes all upstack and downtrack branches,
// as well as the branch itself.
func (h *Handler) RestackStack(ctx context.Context, branch string) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		Scope:           ScopeStack,
		ContinueCommand: []string{"stack", "restack"},
	})
	return err
}
