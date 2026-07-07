package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"path"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeTemplatePaths reports the paths at which change templates
// can be found in a Bitbucket repository.
func (*Forge) ChangeTemplatePaths() []string {
	// Bitbucket does not have native PR template support like GitHub/GitLab.
	// Some repositories use community conventions.
	return []string{
		"PULL_REQUEST_TEMPLATE.md",
		"pull_request_template.md",
		".bitbucket/PULL_REQUEST_TEMPLATE.md",
		".bitbucket/pull_request_template.md",
	}
}

// ListChangeTemplates reads templates from well-known paths.
func (r *Repository) ListChangeTemplates(
	ctx context.Context,
) ([]*forge.ChangeTemplate, error) {
	var out []*forge.ChangeTemplate
	for _, p := range r.forge.ChangeTemplatePaths() {
		body, err := r.gw.ChangeTemplate(ctx, p)
		if err != nil {
			if errors.Is(err, forge.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("get template %q: %w", p, err)
		}

		out = append(out, &forge.ChangeTemplate{
			Filename: path.Base(p),
			Body:     body,
		})
	}
	return out, nil
}
