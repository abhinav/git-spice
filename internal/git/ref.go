package git

import (
	"context"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

// Refspec specifies which refs to fetch/submit for fetch/push operations.
// See git-fetch(1) and git-push(1) for more information.
type Refspec string

func (r Refspec) String() string {
	return string(r)
}

// Matches checks if a branch name matches a fetch refspec pattern.
//
// The refspec should be in the format used by git fetch, e.g.:
//   - "refs/heads/foo/*" (pattern)
//   - "+refs/heads/foo/*:refs/remotes/origin/foo/*" (full refspec with destination)
//   - "refs/heads/master" (exact match)
//
// For fetch refspecs with a destination (containing ':'),
// only the source side (left of ':') is used for matching.
//
// This implements the same algorithm as Git:
// https://github.com/git/git/blob/f0ef5b6d9bcc258e4cbef93839d1b7465d5212b9/refspec.c#L298-L333
func (r Refspec) Matches(ref string) bool {
	pattern := strings.TrimPrefix(string(r), "+") // "+" prefix is optional

	// For refspecs with destination, extract source side (left of ':').
	// E.g.
	// "+refs/heads/foo/*:refs/remotes/origin/foo/*" -> "refs/heads/foo/*"
	pattern, _, _ = strings.Cut(pattern, ":")

	prefix, suffix, ok := strings.Cut(pattern, "*")
	if !ok {
		// If this is not a pattern refspec (no '*'), do exact match
		return pattern == ref
	}

	prefixLen := len(prefix)
	suffixLen := len(suffix)
	refLen := len(ref)

	// Match if:
	// 1. branchName starts with prefix
	// 2. branchName is long enough for prefix + suffix
	// 3. branchName ends with suffix
	return refLen >= prefixLen+suffixLen &&
		strings.HasPrefix(ref, prefix) &&
		strings.HasSuffix(ref, suffix)
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
	return r.gitCmd(ctx, args...).Run()
}
