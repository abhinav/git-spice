package state

import (
	"context"
	"fmt"
)

const _templatesJSON = "templates"

type templateState struct {
	CacheKey  string           `json:"key"`
	Templates []changeTemplate `json:"templates"`
}

type changeTemplate struct {
	Filename string `json:"filename"`
	Body     string `json:"body"`
}

// CachedTemplate is a change template cached in the git spice store.
type CachedTemplate struct {
	// Filename is the name of the template file.
	//
	// This is NOT the path, and is not guaranteed
	// to correspond to a file on disk.
	Filename string

	// Body is the content of the template file.
	Body string
}

// LoadCachedTemplates returns the cached templates and the key used to cache them.
// Returns [ErrNotExist] if there are no cached templates.
//
// Caller should check the cache key to ensure the templates are still valid.
func (s *Store) LoadCachedTemplates(ctx context.Context) (cacheKey string, ts []*CachedTemplate, err error) {
	var state templateState
	if err := s.db.Get(ctx, _templatesJSON, &state); err != nil {
		return "", nil, fmt.Errorf("load template state: %w", err)
	}

	cacheKey = state.CacheKey
	ts = make([]*CachedTemplate, len(state.Templates))
	for i, t := range state.Templates {
		ts[i] = &CachedTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}

	return cacheKey, ts, nil
}

// CacheTemplates caches the given templates with the given cache key.
// If there's existing cached data, it will be overwritten.
func (s *Store) CacheTemplates(ctx context.Context, cacheKey string, ts []*CachedTemplate) error {
	state := templateState{
		CacheKey:  cacheKey,
		Templates: make([]changeTemplate, len(ts)),
	}
	for i, t := range ts {
		state.Templates[i] = changeTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}

	if err := s.db.Set(ctx, _templatesJSON, state, "cache templates"); err != nil {
		return fmt.Errorf("cache templates: %w", err)
	}

	return nil
}
