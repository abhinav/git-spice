package github

import (
	"context"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	var q struct {
		Repository struct {
			PullRequestTemplates []struct {
				Filename githubv4.String `graphql:"filename"`
				Body     githubv4.String `graphql:"body"`
			} `graphql:"pullRequestTemplates"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	if err := r.client.Query(ctx, &q, map[string]any{
		"owner": githubv4.String(r.owner),
		"name":  githubv4.String(r.repo),
	}); err != nil {
		return nil, err
	}

	out := make([]*forge.ChangeTemplate, 0, len(q.Repository.PullRequestTemplates))
	for _, t := range q.Repository.PullRequestTemplates {
		if t.Body != "" {
			out = append(out, &forge.ChangeTemplate{
				Filename: string(t.Filename),
				Body:     string(t.Body),
			})
		}
	}

	return out, nil
}
