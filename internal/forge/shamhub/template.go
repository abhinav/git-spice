package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/ioutil"
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

var _ = shamhubHandler("GET /{owner}/{repo}/change-template", (*ShamHub).handleChangeTemplate)

type changeTemplateResponse []*changeTemplate

type changeTemplate struct {
	Filename string `json:"filename,omitempty"`
	Body     string `json:"body,omitempty"`
}

func (sh *ShamHub) handleChangeTemplate(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner, and repo are required", http.StatusBadRequest)
		return
	}

	// If the repository has a .shamhub/CHANGE_TEMPLATE.md file,
	// that's the template to use.
	logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
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

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *forgeRepository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	u := f.apiURL.JoinPath(f.owner, f.repo, "change-template")
	var res changeTemplateResponse
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
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
