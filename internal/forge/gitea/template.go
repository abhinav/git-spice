package gitea

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	var templates []*forge.ChangeTemplate
	for _, path := range r.forge.ChangeTemplatePaths() {
		content, err := r.fetchTemplate(ctx, path)
		if err != nil {
			if errors.Is(err, giteagw.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("fetch template %q: %w", path, err)
		}
		templates = append(templates, &forge.ChangeTemplate{
			Filename: templateFilename(path),
			Body:     content,
		})
	}
	return templates, nil
}

func (r *Repository) fetchTemplate(ctx context.Context, path string) (string, error) {
	f, _, err := r.client.FileContent(ctx, r.owner, r.repo, path)
	if err != nil {
		return "", err
	}

	body := f.Content
	if f.Encoding == "base64" {
		// Gitea returns base64 content with possible embedded newlines.
		clean := strings.ReplaceAll(body, "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return "", fmt.Errorf("decode base64 content: %w", err)
		}
		body = string(decoded)
	}
	return body, nil
}

func templateFilename(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
