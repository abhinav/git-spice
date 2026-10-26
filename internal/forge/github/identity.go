package github

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/gateway/github"
)

// identityIDs returns user and team IDs aligned with the supplied identities.
// Successful lookups are cached, including successes from a batch that also
// contains identities GitHub does not recognize.
func (r *Repository) identityIDs(
	ctx context.Context,
	users []string,
	teams []github.TeamName,
) ([]github.ID, []github.ID, error) {
	r.identityIDsMu.RLock()
	userIDs, missingUsers := cachedIdentityIDs(r.userIDsCache, users)
	teamIDs, missingTeams := cachedIdentityIDs(r.teamIDsCache, teams)
	r.identityIDsMu.RUnlock()

	if len(missingUsers) > 0 || len(missingTeams) > 0 {
		r.identityIDsMu.Lock()
		defer r.identityIDsMu.Unlock()

		// Recheck missing items after write lock is acquired
		// in case another goroutine already cached them.
		_, missingUsers = cachedIdentityIDs(r.userIDsCache, missingUsers)
		_, missingTeams = cachedIdentityIDs(r.teamIDsCache, missingTeams)

		if len(missingUsers) > 0 || len(missingTeams) > 0 {
			resolvedUserIDs, resolvedTeamIDs, err := r.gateway.IdentityIDs(
				ctx, missingUsers, missingTeams,
			)
			if err != nil {
				return nil, nil, err
			}

			for i, user := range missingUsers {
				if resolvedUserIDs[i] != "" {
					r.userIDsCache[user] = resolvedUserIDs[i]
					r.log.Debug(
						"Resolved user ID",
						"username", user,
						"id", resolvedUserIDs[i],
					)
				}
			}
			for i, team := range missingTeams {
				if resolvedTeamIDs[i] != "" {
					r.teamIDsCache[team] = resolvedTeamIDs[i]
					r.log.Debug(
						"Resolved team ID",
						"team", team.Organization+"/"+team.Slug,
						"id", resolvedTeamIDs[i],
					)
				}
			}
		}

		userIDs, missingUsers = cachedIdentityIDs(r.userIDsCache, users)
		teamIDs, missingTeams = cachedIdentityIDs(r.teamIDsCache, teams)
	}

	// Anything still uncached after miss resolution does not exist.
	// Return one error for each distinct missing identity.
	var errs []error
	for _, user := range missingUsers {
		errs = append(errs, fmt.Errorf("user not found: %q", user))
	}
	for _, team := range missingTeams {
		errs = append(errs, fmt.Errorf("team not found: %q/%q", team.Organization, team.Slug))
	}

	return userIDs, teamIDs, errors.Join(errs...)
}

// cachedIdentityIDs returns IDs aligned with identities and the distinct
// identities absent from cache. The caller owns synchronization for cache.
func cachedIdentityIDs[T comparable](
	cache map[T]github.ID,
	identities []T,
) ([]github.ID, []T) {
	ids := make([]github.ID, len(identities))
	missingSeen := make(map[T]struct{}, len(identities))
	var missing []T
	for i, item := range identities {
		id, ok := cache[item]
		if ok {
			ids[i] = id
			continue
		}
		if _, seen := missingSeen[item]; !seen {
			missing = append(missing, item)
			missingSeen[item] = struct{}{}
		}
	}
	return ids, missing
}
