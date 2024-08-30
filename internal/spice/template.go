package spice

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// ListChangeTemplates returns the Change templates defined in the repository.
// For GitHub, these are PR templates.
func (s *Service) ListChangeTemplates(
	ctx context.Context,
	remoteName string,
	fr forge.Repository,
) ([]*forge.ChangeTemplate, error) {
	// TODO: Should Repo be injected?
	pathSet := make(map[string]struct{})
	for _, p := range fr.Forge().ChangeTemplatePaths() {
		pathSet[p] = struct{}{}

		// Template paths are case-insensitive,
		// so we'll also want to check other variants:
		dir, file := path.Split(p)
		pathSet[path.Join(dir, strings.ToLower(file))] = struct{}{}
		pathSet[path.Join(dir, strings.ToUpper(file))] = struct{}{}
	}
	templatePaths := slices.Sorted(maps.Keys(pathSet))

	// Cache key is a SHA256 hash of the following, in order:
	//   - Number of allowed template paths
	//   - Git SHA of each template path on the trunk branch,
	//     or "0" if the path does not exist on the trunk branch.
	keyHash := sha256.New()
	_, _ = fmt.Fprintf(keyHash, "%d\n", len(templatePaths))
	for _, path := range templatePaths {
		h, err := s.repo.HashAt(ctx, remoteName+"/"+s.store.Trunk(), path)
		if err != nil {
			if errors.Is(err, git.ErrNotExist) {
				_, _ = fmt.Fprintf(keyHash, "0\n")
				continue
			}
			return nil, fmt.Errorf("lookup %q: %w", path, err)
		}
		_, _ = fmt.Fprintf(keyHash, "%s\n", h)
	}

	key := fmt.Sprintf("%x", keyHash.Sum(nil))
	if ts, err := s.store.LoadCachedTemplates(ctx, key); err == nil {
		result := make([]*forge.ChangeTemplate, len(ts))
		for i, t := range ts {
			result[i] = &forge.ChangeTemplate{
				Filename: t.Filename,
				Body:     t.Body,
			}
		}
		return result, nil
	}

	// Fetch templates from the forge.
	ts, err := fr.ListChangeTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	cached := make([]*state.CachedTemplate, len(ts))
	for i, t := range ts {
		cached[i] = &state.CachedTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}
	if err := s.store.CacheTemplates(ctx, key, cached); err != nil {
		s.log.Warn("Failed to cache templates", "err", err)
	}

	return ts, nil
}
