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

// LoadCachedTemplates returns the cached templates if the cache key matches.
// Returns [ErrNotExist] if the cache key does not match,
// or there are no cached templates.
func (s *Store) LoadCachedTemplates(ctx context.Context, cacheKey string) ([]*CachedTemplate, error) {
	var state templateState
	if err := s.b.Get(ctx, _templatesJSON, &state); err != nil {
		return nil, fmt.Errorf("load template state: %w", err)
	}

	if state.CacheKey != cacheKey {
		return nil, fmt.Errorf("cache key mismatch: %w", ErrNotExist)
	}

	out := make([]*CachedTemplate, len(state.Templates))
	for i, t := range state.Templates {
		out[i] = &CachedTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}

	return out, nil
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

	err := s.b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{
				Key: _templatesJSON,
				Val: state,
			},
		},
		Msg: "cache templates",
	})
	if err != nil {
		return fmt.Errorf("cache templates: %w", err)
	}

	return nil
}
