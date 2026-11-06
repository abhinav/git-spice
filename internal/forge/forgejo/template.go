package forgejo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// ChangeTemplatePaths reports the allowed paths for possible PR templates.
func (*Forge) ChangeTemplatePaths() []string {
	return []string{
		"PULL_REQUEST_TEMPLATE.md",
		"pull_request_template.md",
		// Forgejo checks both .forgejo and the inherited .gitea directory.
		// Codeberg runs Forgejo and does not define a separate template path.
		".forgejo/PULL_REQUEST_TEMPLATE",
		".forgejo/PULL_REQUEST_TEMPLATE.md",
		".forgejo/pull_request_template.md",
		".gitea/PULL_REQUEST_TEMPLATE",
		".gitea/PULL_REQUEST_TEMPLATE.md",
		".gitea/pull_request_template.md",
	}
}

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(
	ctx context.Context,
) ([]*forge.ChangeTemplate, error) {
	var templates []*forge.ChangeTemplate
	for _, templatePath := range r.Forge().ChangeTemplatePaths() {
		if !strings.HasSuffix(templatePath, ".md") {
			dirTemplates, err := r.listTemplatesInDirectory(ctx, templatePath)
			if err != nil {
				return nil, err
			}
			templates = append(templates, dirTemplates...)
			continue
		}

		template, err := r.templateFromPath(ctx, templatePath)
		if err != nil {
			if errors.Is(err, forgejo.ErrNotFound) {
				continue
			}
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func (r *Repository) listTemplatesInDirectory(
	ctx context.Context,
	dir string,
) ([]*forge.ChangeTemplate, error) {
	contents, _, err := r.client.ContentsList(ctx, r.owner, r.repo, dir)
	if err != nil {
		if errors.Is(err, forgejo.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("list templates in %q: %w", dir, err)
	}

	var templates []*forge.ChangeTemplate
	for _, entry := range contents {
		if entry.Type != "file" || !strings.HasSuffix(entry.Name, ".md") {
			continue
		}
		template, err := r.templateFromPath(ctx, entry.Path)
		if err != nil {
			if errors.Is(err, forgejo.ErrNotFound) {
				continue
			}
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}

func (r *Repository) templateFromPath(
	ctx context.Context,
	templatePath string,
) (*forge.ChangeTemplate, error) {
	content, _, err := r.client.ContentsGet(
		ctx,
		r.owner,
		r.repo,
		templatePath,
	)
	if err != nil {
		return nil, fmt.Errorf("get template %q: %w", templatePath, err)
	}

	body, err := decodeContent(content)
	if err != nil {
		return nil, fmt.Errorf("decode template %q: %w", templatePath, err)
	}
	return &forge.ChangeTemplate{
		Filename: path.Base(templatePath),
		Body:     body,
	}, nil
}

func decodeContent(content *forgejo.ContentsResponse) (string, error) {
	if content.Encoding == "" {
		return content.Content, nil
	}
	if strings.EqualFold(content.Encoding, "base64") {
		raw := strings.ReplaceAll(content.Content, "\n", "")
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("unsupported encoding %q", content.Encoding)
}
