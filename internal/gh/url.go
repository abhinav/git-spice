package gh

import (
	"fmt"
	"net/url"
	"strings"
)

// RepoInfo contains information about a GitHub repository.
type RepoInfo struct {
	Owner string
	Name  string
}

func (r RepoInfo) String() string {
	return r.Owner + "/" + r.Name
}

// ParseRepoInfo guesses the GitHub repository owner and name
// from a Git remote URL.
func ParseRepoInfo(remote string) (RepoInfo, error) {
	// We recognize the following GitHub remote URL formats:
	//
	//	http(s)://github.com/OWNER/REPO.git
	//	git@github.com:OWNER/REPO.git
	//
	// We can parse these all with url.Parse
	// if we normalize the latter to:
	//
	//	ssh://git@github.com/OWNER/REPO.git
	if !hasGitProtocol(remote) && strings.Contains(remote, ":") {
		// $user@$host:$path => ssh://$user@$host/$path
		remote = "ssh://" + strings.Replace(remote, ":", "/", 1)
	}

	u, err := url.Parse(remote)
	if err != nil {
		return RepoInfo{}, fmt.Errorf("parse remote URL: %w", err)
	}
	// TODO: We currently assume "github.com" is the host and don't check.
	// In the future, we'll want to validate against the configured
	// GitHub host (e.g. GitHub Enterprise).

	s := u.Path                       // /OWNER/REPO.git
	s = strings.TrimPrefix(s, "/")    // OWNER/REPO.git
	s = strings.TrimSuffix(s, ".git") // OWNER/REPO

	owner, repo, ok := strings.Cut(s, "/")
	if !ok {
		return RepoInfo{}, fmt.Errorf("path %q does not contain a GitHub repo", s)
	}

	return RepoInfo{Owner: owner, Name: repo}, nil
}

// _gitProtocols is a list of known git protocols
// including the :// suffix.
var _gitProtocols = []string{
	"ssh",
	"git",
	"git+ssh",
	"git+https",
	"git+http",
	"https",
	"http",
}

func init() {
	for i, proto := range _gitProtocols {
		_gitProtocols[i] = proto + "://"
	}
}

func hasGitProtocol(url string) bool {
	for _, proto := range _gitProtocols {
		if strings.HasPrefix(url, proto) {
			return true
		}
	}
	return false
}
