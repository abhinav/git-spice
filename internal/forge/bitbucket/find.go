package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
)

// FindChangesByBranch finds pull requests by source branch name.
func (r *Repository) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts forge.FindChangesOptions,
) ([]*forge.FindChangeItem, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	prs, err := r.gw.FindChangesByBranch(ctx, branch, gw.FindChangesOptions{
		State:          opts.State,
		PushRepository: opts.PushRepository,
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]*forge.FindChangeItem, len(prs))
	for i, pr := range prs {
		items[i] = findChangeItem(pr)
	}
	return items, nil
}

// FindChangeByID finds a pull request by its ID.
func (r *Repository) FindChangeByID(
	ctx context.Context,
	id forge.ChangeID,
) (*forge.FindChangeItem, error) {
	pr, err := r.gw.GetChange(ctx, mustPR(id).Number)
	if err != nil {
		return nil, err
	}
	return findChangeItem(pr), nil
}

func findChangeItem(pr *gw.PullRequest) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:        &PR{Number: pr.Number},
		URL:       pr.URL,
		State:     pr.State,
		Subject:   pr.Subject,
		BaseName:  pr.BaseName,
		HeadHash:  pr.HeadHash,
		Draft:     pr.Draft,
		Reviewers: pr.Reviewers,
	}
}
