package azuredevops

import (
	"context"
	"errors"
	"path"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"golang.org/x/sync/errgroup"
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
//
// Candidate paths are checked concurrently since Azure DevOps'
// REST API requires a separate request per candidate path.
func (r *Repository) ListChangeTemplates(
	ctx context.Context,
) ([]*forge.ChangeTemplate, error) {
	var (
		mu        sync.Mutex
		templates []*forge.ChangeTemplate
	)
	add := func(ts ...*forge.ChangeTemplate) {
		mu.Lock()
		defer mu.Unlock()
		templates = append(templates, ts...)
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, filePath := range _changeTemplateFiles {
		g.Go(func() error {
			template, err := r.getChangeTemplateFile(ctx, filePath)
			if err != nil {
				if errors.Is(err, azuredevops.ErrNotFound) {
					return nil
				}
				// A single candidate path failing (e.g. a
				// timeout) should not discard templates
				// already found via other candidate paths.
				r.log.Warn("could not check for change template",
					"path", filePath, "error", err)
				return nil
			}
			add(template)
			return nil
		})
	}

	for _, dir := range _changeTemplateDirs {
		g.Go(func() error {
			dirTemplates, err := r.listChangeTemplateDir(ctx, dir)
			if err != nil {
				if errors.Is(err, azuredevops.ErrNotFound) {
					return nil
				}
				r.log.Warn("could not list change templates",
					"dir", dir, "error", err)
				return nil
			}
			add(dirTemplates...)
			return nil
		})
	}

	// Errors from individual candidates are already handled above,
	// so g.Wait can only fail on unexpected panics being recovered
	// by errgroup, which never happens here.
	_ = g.Wait()
	return templates, nil
}

func (r *Repository) listChangeTemplateDir(
	ctx context.Context,
	dir string,
) ([]*forge.ChangeTemplate, error) {
	items, err := r.gateway.Items(ctx, r.project(), r.repositoryID(), dir)
	if err != nil {
		return nil, err
	}

	var templates []*forge.ChangeTemplate
	for _, item := range items {
		if item.Path == "" || item.Folder {
			continue
		}
		if !isChangeTemplateFile(item.Path) {
			continue
		}

		template, err := r.getChangeTemplateFile(ctx, item.Path)
		if err != nil {
			if errors.Is(err, azuredevops.ErrNotFound) {
				continue
			}
			r.log.Warn("could not fetch change template",
				"path", item.Path, "error", err)
			continue
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func (r *Repository) getChangeTemplateFile(
	ctx context.Context,
	filePath string,
) (*forge.ChangeTemplate, error) {
	item, err := r.gateway.Item(ctx, r.project(), r.repositoryID(), filePath)
	if err != nil {
		return nil, err
	}

	return &forge.ChangeTemplate{
		Filename: path.Base(strings.TrimPrefix(filePath, "/")),
		Body:     item.Content,
	}, nil
}

func isChangeTemplateFile(filePath string) bool {
	return strings.HasSuffix(filePath, ".md") ||
		strings.HasSuffix(filePath, ".txt")
}
