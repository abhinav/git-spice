package forge

import (
	"fmt"
	"iter"
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
	if remoteURL == nil {
		return nil, fmt.Errorf("%w: remote URL is required", ErrUnsupportedURL)
	}

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
		if !remoteURLMatches(d.BaseURL(), remoteURL) {
			continue
		}

		f, err := d.New(remoteURL)
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
