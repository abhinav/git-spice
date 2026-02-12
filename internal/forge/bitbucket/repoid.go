package bitbucket

import (
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// RepositoryID is a unique identifier for a Bitbucket repository.
type RepositoryID struct {
	url       string // required
	workspace string // required
	name      string // required
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

func mustRepositoryID(id forge.RepositoryID) *RepositoryID {
	rid, ok := id.(*RepositoryID)
	if ok {
		return rid
	}
	panic(fmt.Sprintf("expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return fmt.Sprintf("%s/%s", rid.workspace, rid.name)
}

// ChangeURL returns the URL for a Pull Request hosted on Bitbucket.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	prNum := mustPR(id).Number
	return fmt.Sprintf(
		"%s/%s/%s/pull-requests/%v",
		rid.url, rid.workspace, rid.name, prNum,
	)
}
