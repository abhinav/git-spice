package restack

import "context"

// RestackDownstack restacks the downstack of the given branch.
// This includes the branch itself.
func (h *Handler) RestackDownstack(
	ctx context.Context, branch string, opts *Options,
) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		Scope:           ScopeDownstack,
		ContinueCommand: []string{"downstack", "restack"},
		AutoResolve:     opts.autoResolvePtr(),
	})
	return err
}
