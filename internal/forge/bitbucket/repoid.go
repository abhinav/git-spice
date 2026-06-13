package bitbucket

import (
	"fmt"
	"slices"
	"strings"

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

// serverRepositoryID is a Bitbucket Data Center repository ID.
type serverRepositoryID struct {
	url        string // required
	projectKey string // required
	slug       string // required

	// personal reports whether this is a personal ("~user") repository;
	// when true, projectKey holds the username.
	personal bool
}

var _ forge.RepositoryID = (*serverRepositoryID)(nil)

func mustServerRepositoryID(id forge.RepositoryID) *serverRepositoryID {
	rid, ok := id.(*serverRepositoryID)
	if ok {
		return rid
	}
	panic(fmt.Sprintf("bitbucket: expected *serverRepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *serverRepositoryID) String() string {
	if rid.personal {
		return fmt.Sprintf("~%s/%s", rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/%s", rid.projectKey, rid.slug)
}

// ChangeURL returns the web URL for a Data Center pull request.
func (rid *serverRepositoryID) ChangeURL(id forge.ChangeID) string {
	prNum := mustPR(id).Number
	return fmt.Sprintf(
		"%s/pull-requests/%d/overview",
		rid.webBase(), prNum,
	)
}

func (rid *serverRepositoryID) webBase() string {
	if rid.personal {
		return fmt.Sprintf("%s/users/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
}

// parseServerRepoPath parses project/repo and /scm/project/repo paths.
// Personal repositories use ~user/repo.
func parseServerRepoPath(path string) (projectKey, slug string, personal bool, err error) {
	errInvalid := fmt.Errorf(
		"path %q does not contain a Bitbucket Data Center repository", path,
	)

	s := strings.Trim(path, "/")
	s = strings.TrimSuffix(s, ".git")

	segments := strings.Split(s, "/")
	if i := slices.Index(segments, "scm"); i >= 0 {
		segments = segments[i+1:]
	}

	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", "", false, errInvalid
	}
	owner, slug := segments[0], segments[1]

	if rest, ok := strings.CutPrefix(owner, "~"); ok {
		if rest == "" {
			return "", "", false, errInvalid
		}
		return rest, slug, true, nil
	}

	return owner, slug, false, nil
}
