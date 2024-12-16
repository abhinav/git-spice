package gitlab

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeTemplatePaths reports the allowed paths for possible MR templates.
//
// Ref https://docs.gitlab.com/ee/user/project/description_templates.html#create-a-merge-request-template.
func (f *Forge) ChangeTemplatePaths() []string {
	return []string{
		".gitlab/merge_request_templates",
	}
}

// ListChangeTemplates returns MR templates defined in the repository.
func (r *Repository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	const templateType = "merge_requests"

	templates, _, err := r.client.ProjectTemplates.ListTemplates(
		r.repoID, templateType, nil,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	var out []*forge.ChangeTemplate
	for _, t := range templates {
		template, _, err := r.client.ProjectTemplates.GetProjectTemplate(
			r.repoID, templateType, t.Name,
			gitlab.WithContext(ctx),
		)
		if err != nil {
			continue
		}
		if template.Content != "" {
			out = append(out, &forge.ChangeTemplate{
				Filename: template.Name,
				Body:     template.Content,
			})
		}
	}

	return out, nil
}
