package github

import (
	"context"
	"strconv"
)

// ChangeID is a unique identifier for a change in a repository.
type ChangeID int

func (id ChangeID) String() string {
	return "#" + strconv.Itoa(int(id))
}

// IsMerged reports whether a change has been merged.
func (f *Forge) IsMerged(ctx context.Context, id ChangeID) (bool, error) {
	merged, _, err := f.client.PullRequests.IsMerged(ctx, f.owner, f.repo, int(id))
	return merged, err
}
