package git

import (
	"context"
)

// SetRefRequest is a request to set a ref to a new hash.
type SetRefRequest struct {
	// Ref is the name of the ref to set.
	// If the ref is a branch or tag, it should be fully qualified
	// (e.g., "refs/heads/main" or "refs/tags/v1.0").
	Ref string

	// Hash is the hash to set the ref to.
	Hash Hash

	// OldHash, if set, specifies the current value of the ref.
	// The ref will only be updated if it currently points to OldHash.
	// Set this to ZeroHash to ensure that a ref being created
	// does not already exist.
	OldHash Hash
}

// SetRef changes the value of a ref to a new hash.
//
// It optionally allows verifying the current value of the ref
// before updating it.
func (r *Repository) SetRef(ctx context.Context, req SetRefRequest) error {
	// git update-ref <rev> <newvalue> [<oldvalue>]
	args := []string{"update-ref", req.Ref, string(req.Hash)}
	if req.OldHash != "" {
		args = append(args, string(req.OldHash))
	}

	return r.gitCmd(ctx, args...).Run(r.exec)
}
