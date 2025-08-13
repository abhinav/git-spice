package shamhub

import (
	"context"
	"fmt"
	"os/exec"

	"go.abhg.dev/gs/internal/silog"
)

type refExistsRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Ref string `json:"ref"`
}

type refExistsResponse struct {
	Exists bool `json:"exists"`
}

var _ = shamhubRESTHandler("POST /{owner}/{repo}/ref/exists", (*ShamHub).handleRefExists)

func (sh *ShamHub) handleRefExists(ctx context.Context, req *refExistsRequest) (*refExistsResponse, error) {
	owner, repo := req.Owner, req.Repo

	exists := sh.refExists(ctx, owner, repo, req.Ref)

	return &refExistsResponse{Exists: exists}, nil
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
