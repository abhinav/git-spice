package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
)

// SubmitChange creates a new pull request in the repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	if len(req.Labels) > 0 {
		r.log.Warn(r.gw.Product() +
			" does not support PR labels; ignoring --label flags")
	}
	if len(req.Assignees) > 0 {
		r.log.Warn(r.gw.Product() +
			" does not support PR assignees; ignoring --assign flags")
	}

	pr, err := r.gw.CreateChange(ctx, gw.CreateChangeRequest{
		Subject:        req.Subject,
		Body:           req.Body,
		Base:           req.Base,
		Head:           req.Head,
		PushRepository: req.PushRepository,
		Draft:          req.Draft,
		Reviewers:      req.Reviewers,
	})
	if err != nil {
		return forge.SubmitChangeResult{}, err
	}

	return forge.SubmitChangeResult{
		ID:  &PR{Number: pr.Number},
		URL: pr.URL,
	}, nil
}
