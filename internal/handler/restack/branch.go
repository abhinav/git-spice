package restack

import "context"

// RestackBranch restacks the given branch onto its base.
func (h *Handler) RestackBranch(
	ctx context.Context, branch string, opts *Options,
) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		ContinueCommand: []string{"branch", "restack"},
		AutoResolve:     opts.autoResolvePtr(),
	})
	return err
}
