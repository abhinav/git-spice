package restack

import (
	"cmp"
	"context"
)

// UpstackOptions holds options for restacking the upstack of a branch.
type UpstackOptions struct {
	Options

	// SkipStart indicates that the starting branch should not be restacked.
	SkipStart bool `help:"Do not restack the starting branch"`
}

// UpstackRequest is a request to restack a branch and its upstack.
type UpstackRequest struct {
	// Branch is the bottom of the upstack to restack.
	Branch string // required

	Options *UpstackOptions // optional
}

// RestackUpstack restacks the upstack of the given branch,
// including the branch itself, unless SkipStart is set.
func (h *Handler) RestackUpstack(ctx context.Context, req *UpstackRequest) error {
	opts := cmp.Or(req.Options, &UpstackOptions{})
	restackReq := &Request{
		Branch:          req.Branch,
		Scope:           ScopeUpstack,
		ContinueCommand: []string{"upstack", "restack"},
	}
	if opts.SkipStart {
		restackReq.Scope = ScopeUpstackExclusive
		restackReq.ContinueCommand = []string{"upstack", "restack", "--skip-start"}
	}
	_, err := h.Restack(ctx, restackReq)
	return err
}
