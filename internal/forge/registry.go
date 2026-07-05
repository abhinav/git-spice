package forge

import (
	"iter"
	"net/url"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/git/giturl"
)

// Registry is a collection of known code forges.
type Registry struct {
	m sync.Map
}

// All returns an iterator over items in the Forge
// in an unspecified order.
func (r *Registry) All() iter.Seq[Forge] {
	return func(yield func(Forge) bool) {
		r.m.Range(func(_, value any) bool {
			return yield(value.(Forge))
		})
	}
}

// Register registers a Forge with the Registry.
// The Forge may be unregistered by calling the returned function.
func (r *Registry) Register(f Forge) (unregister func()) {
	id := f.ID()
	r.m.Store(id, f)
	return func() {
		r.m.Delete(id)
	}
}

// Lookup searches for a registered Forge by ID.
// It returns false if a forge with that ID is not known.
func (r *Registry) Lookup(id string) (Forge, bool) {
	f, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	return f.(Forge), true
}

// FromRemoteURL attempts to match the given remote URL with a registered forge.
// It returns the matched forge and information about the matched repository.
func FromRemoteURL(r *Registry, remoteURL *giturl.URL) (forge Forge, rid RepositoryID, ok bool) {
	for f := range r.All() {
		baseURL, err := url.Parse(f.BaseURL())
		if err != nil {
			continue
		}

		baseHost := baseURL.Hostname()
		remoteHost := remoteURL.Hostname
		// Some forges advertise a base URL such as "https://github.com",
		// while Git remotes use a related SSH hostname like "ssh.github.com".
		// Accept subdomains so these documented SSH hosts still infer
		// the same forge.
		hostMatches := remoteHost == baseHost ||
			strings.HasSuffix(remoteHost, "."+baseHost)
		if !hostMatches {
			continue
		}

		// A base URL without an explicit port describes the forge host,
		// not one transport endpoint.
		// In that case, allow the remote to specify its SSH port.
		basePort := baseURL.Port()
		if basePort != "" && remoteURL.Port != basePort {
			continue
		}

		rid, err := f.ParseRepositoryPath(remoteURL.Path)
		if err == nil {
			return f, rid, true
		}
	}
	return nil, nil, false
}

// SplitRepositoryPath extracts owner and repository name from a URL path.
//
// It strips leading/trailing slashes and the ".git" suffix,
// then splits on the first slash to get owner/repository components.
// For example,
// "/owner/repo.git" returns "owner" and "repo";
// "/workspace/repo/" returns "workspace" and "repo".
func SplitRepositoryPath(path string) (owner, repo string, ok bool) {
	s := strings.TrimPrefix(path, "/")
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")

	owner, repo, ok = strings.Cut(s, "/")
	return owner, repo, ok
}
