package restack

import "context"

// DownstackRequest is a request to restack a branch and its downstack.
type DownstackRequest struct {
	// Branch is the top of the downstack to restack.
	Branch string // required

	Options *Options // optional
}

// RestackDownstack restacks the downstack of the given branch.
// This includes the branch itself.
func (h *Handler) RestackDownstack(ctx context.Context, req *DownstackRequest) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          req.Branch,
		Scope:           ScopeDownstack,
		ContinueCommand: []string{"downstack", "restack"},
	})
	return err
}
