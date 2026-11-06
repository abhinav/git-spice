package bitbucket

import (
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// RepositoryID is a unique identifier for a Bitbucket repository.
type RepositoryID struct {
	url  string
	kind Kind

	// workspace and name identify Bitbucket Cloud repositories.
	workspace string
	name      string

	// projectKey, slug, and personal identify Bitbucket Data Center repositories.
	projectKey string
	slug       string

	// personal reports whether this is a personal ("~user") repository;
	// when true, projectKey holds the username.
	personal bool
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

func mustRepositoryID(id forge.RepositoryID) *RepositoryID {
	rid, ok := id.(*RepositoryID)
	if ok {
		return rid
	}
	panic(fmt.Sprintf("bitbucket: expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	if rid.kind == KindCloud {
		return fmt.Sprintf("%s/%s", rid.workspace, rid.name)
	}

	if rid.personal {
		return fmt.Sprintf("~%s/%s", rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/%s", rid.projectKey, rid.slug)
}

// ChangeURL returns the URL for a Pull Request hosted on Bitbucket.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	prNum := mustPR(id).Number
	if rid.kind == KindCloud {
		return fmt.Sprintf(
			"%s/%s/%s/pull-requests/%v",
			rid.url, rid.workspace, rid.name, prNum,
		)
	}

	return fmt.Sprintf(
		"%s/pull-requests/%d/overview",
		rid.webBase(), prNum,
	)
}

func (rid *RepositoryID) webBase() string {
	if rid.personal {
		return fmt.Sprintf("%s/users/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
}
