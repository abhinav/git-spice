package forge

import (
	"fmt"
	"iter"
	"net/url"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/git/giturl"
)

// Registry is a collection of known code forge definitions.
type Registry struct {
	m sync.Map
}

// All returns an iterator over definitions in the Registry
// in an unspecified order.
func (r *Registry) All() iter.Seq[Definition] {
	return func(yield func(Definition) bool) {
		r.m.Range(func(_, value any) bool {
			return yield(value.(Definition))
		})
	}
}

// Register registers a forge definition with the Registry.
// The definition may be unregistered by calling the returned function.
func (r *Registry) Register(d Definition) (unregister func()) {
	id := d.ID()
	r.m.Store(id, d)
	return func() {
		r.m.Delete(id)
	}
}

// Lookup searches for a registered forge definition by ID.
// It returns false if a forge with that ID is not known.
func (r *Registry) Lookup(id string) (Definition, bool) {
	d, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	return d.(Definition), true
}

// New constructs a registered forge by ID.
func (r *Registry) New(id string, remoteURL *giturl.URL) (Forge, error) {
	d, ok := r.Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	return d.New(remoteURL)
}

// InferFromRemoteURL attempts to infer the forge for the given remote URL.
// It returns the matched forge and information about the matched repository.
func InferFromRemoteURL(r *Registry, remoteURL *giturl.URL) (forge Forge, rid RepositoryID, ok bool) {
	for d := range r.All() {
		f, err := d.New(nil)
		if err != nil {
			continue
		}

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

		f, err = d.New(remoteURL)
		if err != nil {
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
