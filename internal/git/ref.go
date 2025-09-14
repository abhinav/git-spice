package git

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
)

// Refspec specifies which refs to fetch/submit for fetch/push operations.
// See git-fetch(1) and git-push(1) for more information.
type Refspec string

func (r Refspec) String() string {
	return string(r)
}

// SetRefRequest is a request to set a ref to a new hash.
type SetRefRequest struct {
	// Ref is the name of the ref to set.
	// If the ref is a branch or tag, it should be fully qualified
	// (e.g., "refs/heads/main" or "refs/tags/v1.0").
	Ref string // required

	// Hash is the hash to set the ref to.
	Hash Hash // required

	// OldHash, if set, specifies the current value of the ref.
	// The ref will only be updated if it currently points to OldHash.
	// Set this to ZeroHash to ensure that a ref being created
	// does not already exist.
	OldHash Hash

	// Reason, if set, is a human-readable reason for the ref update.
	Reason string
}

// SetRef changes the value of a ref to a new hash.
//
// It optionally allows verifying the current value of the ref
// before updating it.
func (r *Repository) SetRef(ctx context.Context, req SetRefRequest) error {
	// TODO: Add bulk update API with --stdin
	r.log.Debug("Updating Git ref",
		"name", req.Ref,
		"hash", req.Hash,
		silog.NonZero("oldHash", req.OldHash),
	)

	// git update-ref [-m <reason>] <rev> <newvalue> [<oldvalue>]
	args := []string{"update-ref"}
	if req.Reason != "" {
		args = append(args, "-m", req.Reason)
	}
	args = append(args, req.Ref, string(req.Hash))
	if req.OldHash != "" {
		args = append(args, string(req.OldHash))
	}
	return r.gitCmd(ctx, args...).Run(r.exec)
}
