package bitbucket

import (
	"context"
	"fmt"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

// FindChangesByBranch finds pull requests by source branch name.
func (r *Repository) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts forge.FindChangesOptions,
) ([]*forge.FindChangeItem, error) {
	prs, err := r.listPRsByBranch(ctx, branch, opts)
	if err != nil {
		return nil, err
	}
	return r.convertPRsToFindItems(prs), nil
}

func (r *Repository) listPRsByBranch(
	ctx context.Context,
	branch string,
	opts forge.FindChangesOptions,
) ([]apiPullRequest, error) {
	query := fmt.Sprintf(`source.branch.name="%s"`, branch)
	if opts.State != 0 {
		query += fmt.Sprintf(` AND state="%s"`, stateToAPI(opts.State))
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}

	path := fmt.Sprintf(
		"/repositories/%s/%s/pullrequests?q=%s&pagelen=%d&fields=%%2Bvalues.reviewers",
		r.workspace, r.repo, url.QueryEscape(query), limit,
	)

	var resp apiPRList
	if err := r.client.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	return resp.Values, nil
}

// FindChangeByID finds a pull request by its ID.
func (r *Repository) FindChangeByID(
	ctx context.Context,
	id forge.ChangeID,
) (*forge.FindChangeItem, error) {
	pr, err := r.getPullRequest(ctx, mustPR(id).Number)
	if err != nil {
		return nil, err
	}
	return r.convertPRToFindItem(pr), nil
}

func (r *Repository) getPullRequest(
	ctx context.Context,
	prID int64,
) (*apiPullRequest, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", r.workspace, r.repo, prID)

	var pr apiPullRequest
	if err := r.client.get(ctx, path, &pr); err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	return &pr, nil
}

func (r *Repository) convertPRsToFindItems(prs []apiPullRequest) []*forge.FindChangeItem {
	items := make([]*forge.FindChangeItem, len(prs))
	for i := range prs {
		items[i] = r.convertPRToFindItem(&prs[i])
	}
	return items
}

func (r *Repository) convertPRToFindItem(pr *apiPullRequest) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:        &PR{Number: pr.ID},
		URL:       pr.Links.HTML.Href,
		State:     stateFromAPI(pr.State),
		Subject:   pr.Title,
		BaseName:  pr.Destination.Branch.Name,
		HeadHash:  extractHeadHash(pr),
		Draft:     pr.Draft,
		Reviewers: extractUsernames(pr.Reviewers),
	}
}

func extractHeadHash(pr *apiPullRequest) git.Hash {
	if pr.Source.Commit != nil {
		return git.Hash(pr.Source.Commit.Hash)
	}
	if pr.MergeCommit != nil {
		return git.Hash(pr.MergeCommit.Hash)
	}
	return ""
}

func extractUsernames(users []apiUser) []string {
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
func extractUsername(u *apiUser) string {
	if u.Username != "" {
		return u.Username
	}
	return u.Nickname
}
