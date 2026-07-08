package bitbucket

import (
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git/giturl"
)

// deriveInstanceURL returns the web URL for a Data Center remote.
// HTTP(S) remotes preserve the context path before /scm/;
// SSH-style remotes fall back to https://host.
func deriveInstanceURL(u *giturl.URL) string {
	scheme := "https"
	preservePort := false
	switch u.Scheme {
	case "https", "http":
		scheme = u.Scheme
		preservePort = true
	case "git+https":
		preservePort = true
	case "git+http":
		scheme = "http"
		preservePort = true
	}

	derived := scheme + "://" + u.Hostname
	if preservePort && u.Port != "" {
		derived += ":" + u.Port
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if i := slices.Index(segments, "scm"); i > 0 {
		// Data Center HTTP clone URLs may include a web context path
		// before /scm/; keep that prefix in the derived instance URL.
		derived += "/" + strings.Join(segments[:i], "/")
	}

	return derived
}
