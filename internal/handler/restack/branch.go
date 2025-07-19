package restack

import "context"

// RestackBranch restacks the given branch onto its base.
func (h *Handler) RestackBranch(ctx context.Context, branch string) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          branch,
		ContinueCommand: []string{"branch", "restack"},
	})
	return err
}
