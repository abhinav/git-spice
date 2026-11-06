package cloud

import (
	"context"
	"fmt"
	"strings"
)

// resolveReviewerUUIDs resolves reviewer identifiers
// (nicknames or account IDs) to Bitbucket user UUIDs,
// which the pull request API requires.
func (g *Gateway) resolveReviewerUUIDs(
	ctx context.Context,
	usernames []string,
) ([]string, error) {
	if len(usernames) == 0 {
		return nil, nil
	}

	reviewers := make([]string, 0, len(usernames))
	for _, username := range usernames {
		user, err := g.getUser(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("lookup user %q: %w", username, err)
		}
		reviewers = append(reviewers, user.UUID)
		g.log.Debug("Resolved reviewer", "username", username, "uuid", user.UUID)
	}
	return reviewers, nil
}

// getUser finds the workspace member with the given identifier,
// matching by account ID if the identifier looks like one,
// and by nickname otherwise.
func (g *Gateway) getUser(
	ctx context.Context,
	identifier string,
) (*User, error) {
	if isAccountID(identifier) {
		return g.getUserByAccountID(ctx, identifier)
	}
	return g.getUserByNickname(ctx, identifier)
}

func (g *Gateway) getUserByNickname(
	ctx context.Context,
	nickname string,
) (*User, error) {
	user, err := g.findWorkspaceMember(ctx, nickname)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf(
			"user %q not found in workspace %q", nickname, g.workspace,
		)
	}
	return user, nil
}

func (g *Gateway) getUserByAccountID(
	ctx context.Context,
	accountID string,
) (*User, error) {
	user, err := g.findWorkspaceMemberByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf(
			"account_id %q not found in workspace %q", accountID, g.workspace,
		)
	}
	return user, nil
}

// findWorkspaceMemberByAccountID pages through workspace members
// until it finds the member with the given account ID.
// It returns nil without error if no member matches.
func (g *Gateway) findWorkspaceMemberByAccountID(
	ctx context.Context,
	accountID string,
) (*User, error) {
	var path string

	for {
		user, nextPath, err := g.searchMemberPageByAccountID(ctx, path, accountID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			return user, nil
		}
		if nextPath == "" {
			break
		}
		path = nextPath
	}
	return nil, nil
}

func (g *Gateway) searchMemberPageByAccountID(
	ctx context.Context,
	path string,
	accountID string,
) (*User, string, error) {
	var opt *WorkspaceMemberListOptions
	if path != "" {
		opt = &WorkspaceMemberListOptions{PageURL: path}
	}

	members, resp, err := g.client.WorkspaceMemberList(
		ctx,
		g.workspace,
		opt,
	)
	if err != nil {
		return nil, "", fmt.Errorf("list workspace members: %w", err)
	}

	for i := range members.Values {
		member := &members.Values[i]
		if member.User.AccountID == accountID {
			return &member.User, "", nil
		}
	}
	return nil, resp.NextURL, nil
}

// findWorkspaceMember pages through all workspace members
// and collects those matching the given nickname.
// It returns nil without error if no member matches,
// and an error matching *ambiguousUserError
// if several members match.
func (g *Gateway) findWorkspaceMember(
	ctx context.Context,
	nickname string,
) (*User, error) {
	var matches []*User
	var path string

	for {
		pageMatches, nextPath, err := g.searchMemberPage(ctx, path, nickname)
		if err != nil {
			return nil, err
		}
		matches = append(matches, pageMatches...)
		if nextPath == "" {
			break
		}
		path = nextPath
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, &ambiguousUserError{Nickname: nickname, Matches: matches}
	}
}

func (g *Gateway) searchMemberPage(
	ctx context.Context,
	path string,
	nickname string,
) ([]*User, string, error) {
	var opt *WorkspaceMemberListOptions
	if path != "" {
		opt = &WorkspaceMemberListOptions{PageURL: path}
	}

	members, resp, err := g.client.WorkspaceMemberList(
		ctx,
		g.workspace,
		opt,
	)
	if err != nil {
		return nil, "", fmt.Errorf("list workspace members: %w", err)
	}

	var matches []*User
	for i := range members.Values {
		member := &members.Values[i]
		if matchesNickname(&member.User, nickname) {
			matches = append(matches, &member.User)
		}
	}
	return matches, resp.NextURL, nil
}

// matchesNickname checks if the user matches the given nickname.
// It checks Username first (for backward compatibility), then Nickname
// (since Bitbucket deprecated usernames in favor of account IDs).
func matchesNickname(user *User, nickname string) bool {
	if user.Username != "" && strings.EqualFold(user.Username, nickname) {
		return true
	}
	return strings.EqualFold(user.Nickname, nickname)
}

// isAccountID checks if the identifier looks like a Bitbucket account ID.
// Account IDs have the format "number:uuid" (e.g., "712020:f766d886-...").
func isAccountID(identifier string) bool {
	return strings.Contains(identifier, ":")
}

// ambiguousUserError indicates multiple workspace members match the nickname.
type ambiguousUserError struct {
	Nickname string
	Matches  []*User
}

func (e *ambiguousUserError) Error() string {
	var ids []string
	for _, u := range e.Matches {
		ids = append(ids, u.AccountID)
	}
	return fmt.Sprintf(
		"multiple users match %q: %v (use account_id to disambiguate)",
		e.Nickname, ids)
}

// mergeReviewers combines the existing reviewers with the added
// reviewer UUIDs, deduplicated by UUID with first-seen order kept.
func mergeReviewers(existing []User, added []string) []Reviewer {
	seen := make(map[string]bool)
	result := make([]Reviewer, 0, len(existing)+len(added))

	for _, u := range existing {
		if !seen[u.UUID] {
			seen[u.UUID] = true
			result = append(result, Reviewer{UUID: u.UUID})
		}
	}
	for _, rev := range added {
		if !seen[rev] {
			seen[rev] = true
			result = append(result, Reviewer{UUID: rev})
		}
	}
	return result
}

// extractUsernames returns the display usernames of the given users.
func extractUsernames(users []User) []string {
	if len(users) == 0 {
		return nil
	}
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = extractUsername(&u)
	}
	return names
}

// extractUsername returns the username for display purposes.
// Falls back to Nickname since Bitbucket deprecated usernames.
func extractUsername(u *User) string {
	if u.Username != "" {
		return u.Username
	}
	return u.Nickname
}
