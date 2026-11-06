package restack

import "context"

// BranchRequest is a request to restack one branch onto its base.
type BranchRequest struct {
	// Branch is the branch to restack.
	Branch string // required

	Options *Options // optional
}

// RestackBranch restacks the given branch onto its base.
func (h *Handler) RestackBranch(ctx context.Context, req *BranchRequest) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          req.Branch,
		ContinueCommand: []string{"branch", "restack"},
	})
	return err
}
