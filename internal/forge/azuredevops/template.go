package azuredevops

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

var _changeTemplateDirs = []string{
	".azuredevops/pull_request_template",
	".vsts/pull_request_template",
	"docs/pull_request_template",
	"pull_request_template",
}

var _changeTemplateFiles = []string{
	".azuredevops/pull_request_template.md",
	".azuredevops/pull_request_template.txt",
	".vsts/pull_request_template.md",
	".vsts/pull_request_template.txt",
	"docs/pull_request_template.md",
	"docs/pull_request_template.txt",
	"pull_request_template.md",
	"pull_request_template.txt",
}

// ChangeTemplatePaths reports the allowed paths for PR templates.
//
// Azure DevOps looks for templates in the following locations:
//   - /.azuredevops/pull_request_template.md
//   - /.azuredevops/pull_request_template.txt
//   - /.azuredevops/pull_request_template/*.md
//   - /.azuredevops/pull_request_template/*.txt
//   - /.vsts/pull_request_template.*
//   - /docs/pull_request_template.*
//   - /pull_request_template.*
//
// See: https://learn.microsoft.com/azure/devops/repos/git/pull-request-templates
func (f *Forge) ChangeTemplatePaths() []string {
	paths := make([]string, 0, len(_changeTemplateDirs)+len(_changeTemplateFiles))
	paths = append(paths, _changeTemplateDirs...)
	paths = append(paths, _changeTemplateFiles...)
	return paths
}

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(
	ctx context.Context,
) ([]*forge.ChangeTemplate, error) {
	var templates []*forge.ChangeTemplate
	for _, filePath := range _changeTemplateFiles {
		template, err := r.getChangeTemplateFile(ctx, filePath)
		if err != nil {
			if isAzureStatus(err, http.StatusNotFound) {
				continue
			}
			return nil, fmt.Errorf("get template %q: %w", filePath, err)
		}
		templates = append(templates, template)
	}

	for _, dir := range _changeTemplateDirs {
		dirTemplates, err := r.listChangeTemplateDir(ctx, dir)
		if err != nil {
			if isAzureStatus(err, http.StatusNotFound) {
				continue
			}
			return nil, fmt.Errorf("list templates in %q: %w", dir, err)
		}
		templates = append(templates, dirTemplates...)
	}
	return templates, nil
}

func (r *Repository) listChangeTemplateDir(
	ctx context.Context,
	dir string,
) ([]*forge.ChangeTemplate, error) {
	recursionLevel := git.VersionControlRecursionTypeValues.OneLevel
	items, err := r.client.gitClient.GetItems(ctx, git.GetItemsArgs{
		Project:        new(r.project()),
		RepositoryId:   new(r.repositoryID()),
		ScopePath:      &dir,
		RecursionLevel: &recursionLevel,
	})
	if err != nil {
		return nil, err
	}

	var templates []*forge.ChangeTemplate
	for _, item := range *items {
		if item.Path == nil || (item.IsFolder != nil && *item.IsFolder) {
			continue
		}
		if !isChangeTemplateFile(*item.Path) {
			continue
		}

		template, err := r.getChangeTemplateFile(ctx, *item.Path)
		if err != nil {
			return nil, fmt.Errorf("get template file %q: %w", *item.Path, err)
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func (r *Repository) getChangeTemplateFile(
	ctx context.Context,
	filePath string,
) (*forge.ChangeTemplate, error) {
	includeContent := true
	item, err := r.client.gitClient.GetItem(ctx, git.GetItemArgs{
		Project:        new(r.project()),
		RepositoryId:   new(r.repositoryID()),
		Path:           &filePath,
		IncludeContent: &includeContent,
	})
	if err != nil {
		return nil, err
	}

	var body string
	if item.Content != nil {
		body = *item.Content
	}
	return &forge.ChangeTemplate{
		Filename: path.Base(strings.TrimPrefix(filePath, "/")),
		Body:     body,
	}, nil
}

func isChangeTemplateFile(filePath string) bool {
	return strings.HasSuffix(filePath, ".md") ||
		strings.HasSuffix(filePath, ".txt")
}
