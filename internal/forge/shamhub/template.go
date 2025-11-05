package shamhub

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

var _changeTemplatePaths = []string{
	".shamhub/CHANGE_TEMPLATE.md",
	"CHANGE_TEMPLATE.md",
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

func (sh *ShamHub) handleChangeTemplate(_ context.Context, req *changeTemplateRequest) (changeTemplateResponse, error) {
	owner, repo := req.Owner, req.Repo

	// If the repository has a .shamhub/CHANGE_TEMPLATE.md file,
	// that's the template to use.
	logw, flush := silog.Writer(sh.log, silog.LevelDebug)
	defer flush()

	templatePaths := make(map[string]struct{})
	for _, p := range _changeTemplatePaths {
		templatePaths[p] = struct{}{}
		templatePaths[strings.ToLower(p)] = struct{}{}
		templatePaths[strings.ToUpper(p)] = struct{}{}
	}

	var res changeTemplateResponse
	for path := range templatePaths {
		cmd := exec.Command(sh.gitExe, "cat-file", "-p", "HEAD:"+path)
		cmd.Dir = sh.repoDir(owner, repo)
		cmd.Stderr = logw

		if out, err := cmd.Output(); err == nil {
			res = append(res, &changeTemplate{
				Filename: path,
				Body:     strings.TrimSpace(string(out)) + "\n",
			})
		}
	}

	return res, nil
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
