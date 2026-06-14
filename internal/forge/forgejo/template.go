package forgejo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
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
		".forgejo/PULL_REQUEST_TEMPLATE.md",
		".forgejo/pull_request_template.md",
		".gitea/PULL_REQUEST_TEMPLATE.md",
		".gitea/pull_request_template.md",
	}
}

// ListChangeTemplates returns PR templates defined in the repository.
func (r *Repository) ListChangeTemplates(
	ctx context.Context,
) ([]*forge.ChangeTemplate, error) {
	var templates []*forge.ChangeTemplate
	for _, path := range r.Forge().ChangeTemplatePaths() {
		content, _, err := r.client.ContentsGet(ctx, r.owner, r.repo, path)
		if err != nil {
			if errors.Is(err, forgejo.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("get template %q: %w", path, err)
		}

		body, err := decodeContent(content)
		if err != nil {
			return nil, fmt.Errorf("decode template %q: %w", path, err)
		}
		templates = append(templates, &forge.ChangeTemplate{
			Filename: filepath.Base(path),
			Body:     body,
		})
	}
	return templates, nil
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
