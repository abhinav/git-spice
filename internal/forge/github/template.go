package github

import (
	"context"
	"strings"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeTemplatePaths reports the allowed paths for possible PR templates.
//
// Ref https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/creating-a-pull-request-template-for-your-repository.
func (f *Forge) ChangeTemplatePaths() []string {
	paths := []string{
		"PULL_REQUEST_TEMPLATE.md",
		"PULL_REQUEST_TEMPLATE",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/PULL_REQUEST_TEMPLATE",
		"docs/PULL_REQUEST_TEMPLATE.md",
		"docs/PULL_REQUEST_TEMPLATE",
	}

	// GitHub accepts PULL_REQUEST_TEMPLATE and pull_request_template.
	for _, p := range paths {
		paths = append(paths, strings.ToLower(p))
	}

	return paths
}

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
