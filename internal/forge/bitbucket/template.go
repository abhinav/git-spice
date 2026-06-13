package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"path"

	"go.abhg.dev/gs/internal/forge"
)

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
