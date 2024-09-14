package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/ioutil"
)

type areMergedRequest struct {
	IDs []ChangeID `json:"ids"`
}

type areMergedResponse struct {
	Merged []bool `json:"merged"`
}

var _ = shamhubHandler("POST /{owner}/{repo}/change/merged", (*ShamHub).handleAreMerged)

func (sh *ShamHub) handleAreMerged(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner, repo, and number are required", http.StatusBadRequest)
		return
	}

	var req areMergedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	changeNumToIdx := make(map[int]int, len(req.IDs))
	for i, id := range req.IDs {
		changeNumToIdx[int(id)] = i
	}

	sh.mu.RLock()
	merged := make([]bool, len(sh.changes))
	for _, c := range sh.changes {
		if c.Owner == owner && c.Repo == repo {
			idx, ok := changeNumToIdx[c.Number]
			if !ok {
				continue
			}
			merged[idx] = c.State == shamChangeMerged
			delete(changeNumToIdx, c.Number)

			if len(changeNumToIdx) == 0 {
				break
			}
		}
	}
	sh.mu.RUnlock()

	if len(changeNumToIdx) > 0 {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "changes not found: %v", changeNumToIdx)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(areMergedResponse{Merged: merged}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *forgeRepository) ChangesAreMerged(ctx context.Context, fids []forge.ChangeID) ([]bool, error) {
	ids := make([]ChangeID, len(fids))
	for i, fid := range fids {
		ids[i] = fid.(ChangeID)
	}

	u := f.apiURL.JoinPath(f.owner, f.repo, "change", "merged")
	req := areMergedRequest{IDs: ids}

	var res areMergedResponse
	if err := f.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("are merged: %w", err)
	}
	return res.Merged, nil
}

// MergeChangeRequest is a request to merge an open change
// proposed against this forge.
type MergeChangeRequest struct {
	Owner, Repo string
	Number      int

	// Optional fields:
	Time                          time.Time
	CommitterName, CommitterEmail string
	// TODO: Use git.Signature here instead?

	// DeleteBranch indicates that the merged branch
	// should be deleted after the merge.
	DeleteBranch bool

	// TODO: option to use squash commit
}

// MergeChange merges an open change against this forge.
func (sh *ShamHub) MergeChange(req MergeChangeRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return fmt.Errorf("owner, repo, and number are required")
	}

	if req.CommitterName == "" {
		req.CommitterName = "ShamHub"
	}
	if req.CommitterEmail == "" {
		req.CommitterEmail = "shamhub@example.com"
	}

	commitEnv := []string{
		"GIT_COMMITTER_NAME=" + req.CommitterName,
		"GIT_COMMITTER_EMAIL=" + req.CommitterEmail,
		"GIT_AUTHOR_NAME=" + req.CommitterName,
		"GIT_AUTHOR_EMAIL=" + req.CommitterEmail,
	}
	if !req.Time.IsZero() {
		commitEnv = append(commitEnv,
			"GIT_COMMITTER_DATE="+req.Time.Format(time.RFC3339),
			"GIT_AUTHOR_DATE="+req.Time.Format(time.RFC3339),
		)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	var changeIdx int
	for idx, change := range sh.changes {
		if change.Owner == req.Owner && change.Repo == req.Repo && change.Number == req.Number {
			changeIdx = idx
			break
		}
	}

	if sh.changes[changeIdx].State != shamChangeOpen {
		return fmt.Errorf("change %d is not open", req.Number)
	}

	// To do this without a worktree, we need to:
	//
	//	TREE=$(git merge-tree --write-tree base head)
	//
	// If the above fails, there's a conflict, so reject the merge.
	// Otherwise, create a commit with the TREE and the commit message
	// using git commit-tree, and update the ref to point to the new commit.
	//
	// This requires at least Git 2.38.
	tree, err := func() (string, error) {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		cmd := exec.Command(sh.gitExe, "merge-tree", "--write-tree", sh.changes[changeIdx].Base, sh.changes[changeIdx].Head)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("merge-tree: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}()
	if err != nil {
		return err
	}

	commit, err := func() (string, error) {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		msg := fmt.Sprintf("Merge change #%d", req.Number)
		cmd := exec.Command(sh.gitExe,
			"commit-tree",
			"-p", sh.changes[changeIdx].Base,
			"-p", sh.changes[changeIdx].Head,
			"-m", msg,
			tree,
		)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		cmd.Env = append(os.Environ(), commitEnv...)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("commit-tree: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}()
	if err != nil {
		return err
	}

	// Update the ref to point to the new commit.
	err = func() error {
		logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
		defer flush()

		ref := fmt.Sprintf("refs/heads/%s", sh.changes[changeIdx].Base)
		cmd := exec.Command(sh.gitExe, "update-ref", ref, commit)
		cmd.Dir = sh.repoDir(req.Owner, req.Repo)
		cmd.Stderr = logw
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("update-ref: %w", err)
		}

		return nil
	}()
	if err != nil {
		return err
	}

	if req.DeleteBranch {
		err := func() error {
			logw, flush := ioutil.LogWriter(sh.log, log.DebugLevel)
			defer flush()

			cmd := exec.Command(sh.gitExe, "branch", "-D", sh.changes[changeIdx].Head)
			cmd.Dir = sh.repoDir(req.Owner, req.Repo)
			cmd.Stderr = logw
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("delete branch: %w", err)
			}

			return nil
		}()
		if err != nil {
			return fmt.Errorf("delete branch: %w", err)
		}
	}

	sh.changes[changeIdx].State = shamChangeMerged
	return nil
}
