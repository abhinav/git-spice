package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// SubmitChange creates a new pull request in the repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	r.warnUnsupportedFeatures(req)

	reviewers, err := r.resolveReviewerUUIDs(ctx, req.Reviewers)
	if err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("resolve reviewers: %w", err)
	}

	apiReq := r.buildCreatePRRequest(req, reviewers)
	pr, err := r.createPullRequest(ctx, apiReq)
	if err != nil {
		return forge.SubmitChangeResult{}, err
	}

	r.log.Debug("Created pull request", "pr", pr.ID, "url", pr.Links.HTML.Href)
	return forge.SubmitChangeResult{
		ID:  &PR{Number: pr.ID},
		URL: pr.Links.HTML.Href,
	}, nil
}

func (r *Repository) warnUnsupportedFeatures(req forge.SubmitChangeRequest) {
	if len(req.Labels) > 0 {
		r.log.Warn("Bitbucket does not support PR labels; ignoring --label flags")
	}
	if len(req.Assignees) > 0 {
		r.log.Warn("Bitbucket does not support PR assignees; ignoring --assign flags")
	}
}

func (r *Repository) buildCreatePRRequest(
	req forge.SubmitChangeRequest,
	reviewers []string,
) *bitbucket.PullRequestCreateRequest {
	apiReq := &bitbucket.PullRequestCreateRequest{
		Title: req.Subject,
		Source: bitbucket.BranchRef{
			Branch: bitbucket.Branch{Name: req.Head},
		},
		Destination: bitbucket.BranchRef{
			Branch: bitbucket.Branch{Name: req.Base},
		},
		Draft: req.Draft,
	}
	if req.PushRepository != nil {
		apiReq.Source.Repository = &bitbucket.RepositoryRef{
			FullName: req.PushRepository.String(),
		}
	}
	if req.Body != "" {
		apiReq.Description = req.Body
	}
	if len(reviewers) > 0 {
		apiReq.Reviewers = make([]bitbucket.Reviewer, 0, len(reviewers))
		for _, reviewer := range reviewers {
			apiReq.Reviewers = append(apiReq.Reviewers, bitbucket.Reviewer{
				UUID: reviewer,
			})
		}
	}
	return apiReq
}

func (r *Repository) createPullRequest(
	ctx context.Context,
	req *bitbucket.PullRequestCreateRequest,
) (*bitbucket.PullRequest, error) {
	pr, _, err := r.client.PullRequestCreate(ctx, r.workspace, r.repo, req)
	if err != nil {
		if errors.Is(err, bitbucket.ErrDestinationBranchNotFound) {
			return nil, fmt.Errorf("create pull request: %w", forge.ErrUnsubmittedBase)
		}
		return nil, fmt.Errorf("create pull request: %w", err)
	}
	return pr, nil
}

func (r *Repository) resolveReviewerUUIDs(
	ctx context.Context,
	usernames []string,
) ([]string, error) {
	if len(usernames) == 0 {
		return nil, nil
	}

	reviewers := make([]string, 0, len(usernames))
	for _, username := range usernames {
		user, err := r.getUser(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("lookup user %q: %w", username, err)
		}
		reviewers = append(reviewers, user.UUID)
		r.log.Debug("Resolved reviewer", "username", username, "uuid", user.UUID)
	}
	return reviewers, nil
}

func (r *Repository) getUser(ctx context.Context, identifier string) (*bitbucket.User, error) {
	if isAccountID(identifier) {
		return r.getUserByAccountID(ctx, identifier)
	}
	return r.getUserByNickname(ctx, identifier)
}

func (r *Repository) getUserByNickname(
	ctx context.Context,
	nickname string,
) (*bitbucket.User, error) {
	user, err := r.findWorkspaceMember(ctx, nickname)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user %q not found in workspace %q", nickname, r.workspace)
	}
	return user, nil
}

func (r *Repository) getUserByAccountID(
	ctx context.Context,
	accountID string,
) (*bitbucket.User, error) {
	user, err := r.findWorkspaceMemberByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("account_id %q not found in workspace %q", accountID, r.workspace)
	}
	return user, nil
}

func (r *Repository) findWorkspaceMemberByAccountID(
	ctx context.Context,
	accountID string,
) (*bitbucket.User, error) {
	var path string

	for {
		user, nextPath, err := r.searchMemberPageByAccountID(ctx, path, accountID)
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

func (r *Repository) searchMemberPageByAccountID(
	ctx context.Context,
	path string,
	accountID string,
) (*bitbucket.User, string, error) {
	var opt *bitbucket.WorkspaceMemberListOptions
	if path != "" {
		opt = &bitbucket.WorkspaceMemberListOptions{PageURL: path}
	}

	members, resp, err := r.client.WorkspaceMemberList(
		ctx,
		r.workspace,
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

func (r *Repository) findWorkspaceMember(
	ctx context.Context,
	nickname string,
) (*bitbucket.User, error) {
	var matches []*bitbucket.User
	var path string

	for {
		pageMatches, nextPath, err := r.searchMemberPage(ctx, path, nickname)
		if err != nil {
			return nil, err
		}
		matches = append(matches, pageMatches...)
		if nextPath == "" {
			break
		}
		path = nextPath
	}

	return r.selectUniqueMatch(nickname, matches)
}

func (r *Repository) selectUniqueMatch(
	nickname string,
	matches []*bitbucket.User,
) (*bitbucket.User, error) {
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, &ambiguousUserError{Nickname: nickname, Matches: matches}
	}
}

func (r *Repository) searchMemberPage(
	ctx context.Context,
	path string,
	nickname string,
) ([]*bitbucket.User, string, error) {
	var opt *bitbucket.WorkspaceMemberListOptions
	if path != "" {
		opt = &bitbucket.WorkspaceMemberListOptions{PageURL: path}
	}

	members, resp, err := r.client.WorkspaceMemberList(
		ctx,
		r.workspace,
		opt,
	)
	if err != nil {
		return nil, "", fmt.Errorf("list workspace members: %w", err)
	}

	var matches []*bitbucket.User
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
func matchesNickname(user *bitbucket.User, nickname string) bool {
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
	Matches  []*bitbucket.User
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
