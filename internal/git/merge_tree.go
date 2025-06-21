package git

import (
	"bytes"
	"context"
	"fmt"
)

// MergeTreeRequest specifies the parameters for a merge-tree operation.
type MergeTreeRequest struct {
	// Branch1 is the first branch or commit to merge.
	//
	// This must be a commit-ish value if MergeBase is not provided.
	// Otherwise, it can be any tree-ish value.
	Branch1 string // required

	// Branch2 is the second branch or commit to merge.
	//
	// This must be a commit-ish value if MergeBase is not provided.
	// Otherwise, it can be any tree-ish value.
	Branch2 string // required

	// MergeBase optionally specifies an explicit merge base for the merge.
	// If provided, Branch1 and Branch2 can be any tree-ish values.
	//
	// Use of this parameter requires Git 2.40 or later.
	MergeBase string
}

// MergeTree performs a merge without touching the index or working tree,
// returning the hash of the resulting tree.
//
// For conflicts, this method returns an error indicating the conflict occurred.
func (r *Repository) MergeTree(ctx context.Context, req MergeTreeRequest) (Hash, error) {
	args := []string{"merge-tree", "--write-tree", "-z"}
	if req.MergeBase != "" {
		args = append(args, "--merge-base="+req.MergeBase)
	}
	args = append(args, req.Branch1, req.Branch2)

	bs, err := r.gitCmd(ctx, args...).Output(r.exec)
	if err != nil {
		return "", fmt.Errorf("merge-tree %s %s: conflict: %w", req.Branch1, req.Branch2, err)
	}
	// TODO: surface conflict information

	// The -z flag causes output to end with NUL character instead of newline
	// We need to trim it before building the Hash value
	bs = bytes.TrimSuffix(bs, []byte{0})
	return Hash(bs), nil
}
