package azuredevops

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeTemplatePaths reports the allowed paths for PR templates.
//
// Azure DevOps looks for templates in the following locations:
// - /.azuredevops/pull_request_template.md
// - /.vsts/pull_request_template.md
// - /docs/pull_request_template.md
// - /pull_request_template.md
//
// See: https://learn.microsoft.com/azure/devops/repos/git/pull-request-templates
func (f *Forge) ChangeTemplatePaths() []string {
	return []string{
		".azuredevops/pull_request_template.md",
		".azuredevops/pull_request_template.txt",
		".vsts/pull_request_template.md",
		".vsts/pull_request_template.txt",
		"docs/pull_request_template.md",
		"docs/pull_request_template.txt",
		"pull_request_template.md",
		"pull_request_template.txt",
	}
}

// ListChangeTemplates returns PR templates defined in the repository.
//
// Note: Azure DevOps doesn't have an API to fetch PR templates directly.
// Templates are stored as files in the repository and the UI reads them
// from the default branch. For now, we return an empty list and rely on
// the local template detection using ChangeTemplatePaths().
func (r *Repository) ListChangeTemplates(
	_ context.Context,
) ([]*forge.ChangeTemplate, error) {
	// Azure DevOps doesn't have an API to list PR templates.
	// The templates are read from the repository's default branch.
	// Return empty list - the caller will fall back to reading
	// from the local repository using ChangeTemplatePaths().
	return nil, nil
}
