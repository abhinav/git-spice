package restack

import "context"

// StackRequest is a request to restack all branches in a stack.
type StackRequest struct {
	// Branch selects the stack to restack.
	Branch string // required

	Options *Options // optional
}

// RestackStack restacks the stack of the given branch.
// This includes all upstack and downtrack branches,
// as well as the branch itself.
func (h *Handler) RestackStack(ctx context.Context, req *StackRequest) error {
	_, err := h.Restack(ctx, &Request{
		Branch:          req.Branch,
		Scope:           ScopeStack,
		ContinueCommand: []string{"stack", "restack"},
	})
	return err
}
