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

var _changeTemplateDir = ".shamhub/CHANGE_TEMPLATE"

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

	var res changeTemplateResponse

	// Check for templates in .shamhub/CHANGE_TEMPLATE/ directory.
	cmd := exec.Command(sh.gitExe, "ls-tree", "--name-only", "HEAD:"+_changeTemplateDir)
	cmd.Dir = sh.repoDir(owner, repo)
	cmd.Stderr = logw

	if out, err := cmd.Output(); err == nil {
		// Directory exists, list all .md files in it.
		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, file := range files {
			if file == "" {
				continue
			}
			if !strings.HasSuffix(file, ".md") {
				continue
			}

			// Read the file content.
			filePath := _changeTemplateDir + "/" + file
			catCmd := exec.Command(sh.gitExe, "cat-file", "-p", "HEAD:"+filePath)
			catCmd.Dir = sh.repoDir(owner, repo)
			catCmd.Stderr = logw

			if body, err := catCmd.Output(); err == nil {
				res = append(res, &changeTemplate{
					Filename: file,
					Body:     strings.TrimSpace(string(body)) + "\n",
				})
			}
		}
	}

	// Check for single file templates at well-known paths.
	templatePaths := make(map[string]struct{})
	for _, p := range _changeTemplatePaths {
		templatePaths[p] = struct{}{}
		templatePaths[strings.ToLower(p)] = struct{}{}
		templatePaths[strings.ToUpper(p)] = struct{}{}
	}

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
