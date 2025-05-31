package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"

	"go.abhg.dev/gs/internal/silog"
)

type refExistsRequest struct {
	Ref string `json:"ref"`
}

type refExistsResponse struct {
	Exists bool `json:"exists"`
}

var _ = shamhubHandler("POST /{owner}/{repo}/ref/exists", (*ShamHub).handleRefExists)

func (sh *ShamHub) handleRefExists(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	var data refExistsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	exists := sh.refExists(ctx, owner, repo, data.Ref)

	resp := refExistsResponse{Exists: exists}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (r *forgeRepository) RefExists(ctx context.Context, ref string) (bool, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "ref", "exists")
	var res refExistsResponse
	if err := r.client.Post(ctx, u.String(), refExistsRequest{Ref: ref}, &res); err != nil {
		return false, fmt.Errorf("check ref exists: %w", err)
	}
	return res.Exists, nil
}

func (sh *ShamHub) refExists(ctx context.Context, owner, repo, ref string) bool {
	logw, flush := silog.Writer(sh.log, silog.LevelDebug)
	defer flush()

	cmd := exec.CommandContext(ctx, sh.gitExe,
		"show-ref", "--verify", "--quiet", ref)
	cmd.Dir = sh.repoDir(owner, repo)
	cmd.Stderr = logw
	return cmd.Run() == nil
}

func (sh *ShamHub) branchRefExists(ctx context.Context, owner, repo, branch string) bool {
	return sh.refExists(ctx, owner, repo, "refs/heads/"+branch)
}
