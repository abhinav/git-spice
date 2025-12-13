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

func (sh *ShamHub) handleChangeTemplate(_ context.Context, req *changeTemplateRequest) (changeTemplateResponse, error) {
	owner, repo := req.Owner, req.Repo

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
		// Try to read as a file (blob objects only).
		cmd := exec.Command(sh.gitExe, "cat-file", "blob", "HEAD:"+path)
		cmd.Dir = sh.repoDir(owner, repo)
		cmd.Stderr = logw

		if out, err := cmd.Output(); err == nil {
			res = append(res, &changeTemplate{
				Filename: path,
				Body:     strings.TrimSpace(string(out)) + "\n",
			})
			continue
		}

		// Try to read as a directory.
		lsCmd := exec.Command(sh.gitExe, "ls-tree", "--name-only", "HEAD:"+path)
		lsCmd.Dir = sh.repoDir(owner, repo)
		lsCmd.Stderr = logw

		lsOut, err := lsCmd.Output()
		if err != nil {
			continue
		}

		// List all .md files in the directory.
		for file := range strings.SplitSeq(strings.TrimSpace(string(lsOut)), "\n") {
			if file == "" || !strings.HasSuffix(file, ".md") {
				continue
			}

			filePath := path + "/" + file
			catCmd := exec.Command(sh.gitExe, "cat-file", "-p", "HEAD:"+filePath)
			catCmd.Dir = sh.repoDir(owner, repo)
			catCmd.Stderr = logw

			if fileOut, err := catCmd.Output(); err == nil {
				res = append(res, &changeTemplate{
					Filename: file,
					Body:     strings.TrimSpace(string(fileOut)) + "\n",
				})
			}
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
