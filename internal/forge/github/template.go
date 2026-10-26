package github

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeTemplatePaths reports the allowed paths for possible PR templates.
//
// Ref https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/creating-a-pull-request-template-for-your-repository.
func (f *Forge) ChangeTemplatePaths() []string {
	return []string{
		"PULL_REQUEST_TEMPLATE.md",
		"PULL_REQUEST_TEMPLATE",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/PULL_REQUEST_TEMPLATE",
		"docs/PULL_REQUEST_TEMPLATE.md",
		"docs/PULL_REQUEST_TEMPLATE",
	}
}

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	templates, err := r.gateway.ChangeTemplates(ctx, r.owner, r.repo)
	if err != nil {
		return nil, err
	}

	out := make([]*forge.ChangeTemplate, 0, len(templates))
	for _, t := range templates {
		out = append(out, &forge.ChangeTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		})
	}

	return out, nil
}
