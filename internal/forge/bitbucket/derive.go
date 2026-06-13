package bitbucket

import (
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/git/giturl"
)

// ConfigureFromRemoteURL derives the Data Center instance URL
// from a non-Cloud remote.
// It leaves explicit URLs and bitbucket.org remotes unchanged.
func (f *Forge) ConfigureFromRemoteURL(u *giturl.URL) {
	if f.Options.URL != "" {
		return
	}

	if u.Hostname == "" || isCloudHost(u.Hostname) {
		return
	}

	f.Options.URL = deriveInstanceURL(u)
}

// deriveInstanceURL returns the web URL for a Data Center remote.
// HTTP(S) remotes preserve the context path before /scm/;
// SSH-style remotes fall back to https://host.
func deriveInstanceURL(u *giturl.URL) string {
	scheme, ok := webScheme(u.Raw)
	if !ok {
		return "https://" + u.Hostname
	}

	derived := scheme + "://" + u.Hostname
	if u.Port != "" {
		derived += ":" + u.Port
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if i := slices.Index(segments, "scm"); i > 0 {
		derived += "/" + strings.Join(segments[:i], "/")
	}

	return derived
}

// webScheme returns http or https for HTTP(S) Git remotes.
func webScheme(raw string) (string, bool) {
	for _, scheme := range []string{"https", "http"} {
		if strings.HasPrefix(raw, scheme+"://") ||
			strings.HasPrefix(raw, "git+"+scheme+"://") {
			return scheme, true
		}
	}
	return "", false
}
