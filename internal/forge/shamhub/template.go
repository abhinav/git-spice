package shamhub

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/scanutil"
)

var _changeTemplatePaths = []string{
	"CHANGE_TEMPLATE.md",
	"CHANGE_TEMPLATE",
	".shamhub/CHANGE_TEMPLATE.md",
	".shamhub/CHANGE_TEMPLATE",
}

// ChangeTemplatePaths reports the case-insensitive paths at which
// it's possible to define change templates in the repository.
func (f *Forge) ChangeTemplatePaths() []string {
	return slices.Clone(_changeTemplatePaths)
}

type changeTemplateRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`
}

var _ = shamhubRESTHandler("GET /{owner}/{repo}/change-template", (*ShamHub).handleChangeTemplate)

type changeTemplateResponse []*changeTemplate

type changeTemplate struct {
	Filename string `json:"filename,omitempty"`
	Body     string `json:"body,omitempty"`
}

func (sh *ShamHub) handleChangeTemplate(ctx context.Context, req *changeTemplateRequest) (changeTemplateResponse, error) {
	owner, repo := req.Owner, req.Repo

	sh.mu.RLock()
	changeTemplateErrorDelay := sh.changeTemplateErrorDelay
	sh.mu.RUnlock()
	if changeTemplateErrorDelay > 0 {
		select {
		case <-time.After(changeTemplateErrorDelay):
			return nil, &httpError{
				code:    http.StatusInternalServerError,
				message: "change template lookup failed",
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	templatePathSet := make(map[string]struct{}, len(_changeTemplatePaths)*3)
	for _, p := range _changeTemplatePaths {
		templatePathSet[strings.ToLower(p)] = struct{}{}
	}

	var res changeTemplateResponse
	var matches []matchingTemplatePath
	treeCmd := sh.gitCmd(ctx, owner, repo,
		"ls-tree", "-r", "-z", "--name-only", "HEAD")
	for treePath, err := range treeCmd.Scan(scanutil.SplitNull) {
		if err != nil {
			return nil, nil
		}

		templatePath, ok := matchTemplatePath(string(treePath), templatePathSet)
		if !ok {
			continue
		}
		matches = append(matches, templatePath)
	}

	slices.SortFunc(matches, func(a, b matchingTemplatePath) int {
		return strings.Compare(a.filePath, b.filePath)
	})

	for _, templatePath := range matches {
		out, err := sh.gitCmd(ctx, owner, repo,
			"cat-file", "blob", "HEAD:"+templatePath.filePath).
			Output()
		if err != nil {
			continue
		}

		res = append(res, &changeTemplate{
			Filename: templatePath.filename,
			Body:     strings.TrimSpace(string(out)) + "\n",
		})
	}

	return res, nil
}

// matchingTemplatePath identifies an existing repository file
// that should be returned as a change template.
type matchingTemplatePath struct {
	// filePath is the full repository path to read.
	filePath string

	// filename is the template name returned by the ShamHub API.
	// For templates inside a template directory,
	// the API returns only the base name.
	filename string
}

func matchTemplatePath(
	treePath string,
	templatePathSet map[string]struct{},
) (matchingTemplatePath, bool) {
	if treePath == "" {
		return matchingTemplatePath{}, false
	}

	lowerTreePath := strings.ToLower(treePath)

	// ShamHub template paths are case-insensitive,
	// but Git still needs the original path casing when reading the blob.
	if _, ok := templatePathSet[lowerTreePath]; ok {
		return matchingTemplatePath{
			filePath: treePath,
			filename: treePath,
		}, true
	}

	for templatePath := range templatePathSet {
		prefix := templatePath + "/"
		file, ok := strings.CutPrefix(lowerTreePath, prefix)

		// Directory templates include only immediate Markdown children.
		// Nested files and non-Markdown files are not change templates.
		if !ok || strings.Contains(file, "/") || !strings.HasSuffix(file, ".md") {
			continue
		}

		return matchingTemplatePath{
			filePath: treePath,
			filename: path.Base(treePath),
		}, true
	}

	return matchingTemplatePath{}, false
}

func (r *forgeRepository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "change-template")
	var res changeTemplateResponse
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("lookup change body template: %w", err)
	}

	out := make([]*forge.ChangeTemplate, len(res))
	for i, t := range res {
		out[i] = &forge.ChangeTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}

	return out, nil
}
