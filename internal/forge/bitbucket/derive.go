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
	for _, s := range []string{"https", "http"} {
		if strings.HasPrefix(u.Raw, s+"://") ||
			strings.HasPrefix(u.Raw, "git+"+s+"://") {
			scheme = s
			preservePort = true
			break
		}
	}

	derived := scheme + "://" + u.Hostname
	if preservePort && u.Port != "" {
		derived += ":" + u.Port
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if i := slices.Index(segments, "scm"); i > 0 {
		derived += "/" + strings.Join(segments[:i], "/")
	}

	return derived
}
