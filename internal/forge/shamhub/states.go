package shamhub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/xec"
)

type statesRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	IDs []ChangeID `json:"ids"`
}

type statesResponse struct {
	States []string `json:"states"`
}

var _ = shamhubRESTHandler("POST /{owner}/{repo}/change/states", (*ShamHub).handleStates)

func (sh *ShamHub) handleStates(_ context.Context, req *statesRequest) (*statesResponse, error) {
	owner, repo := req.Owner, req.Repo

	changeNumToIdx := make(map[int]int, len(req.IDs))
	for i, id := range req.IDs {
		changeNumToIdx[int(id)] = i
	}

	sh.mu.RLock()
	states := make([]string, len(changeNumToIdx))
	for _, c := range sh.changes {
		if c.Base.Owner == owner && c.Base.Repo == repo {
			idx, ok := changeNumToIdx[c.Number]
			if !ok {
				continue
			}
			switch c.State {
			case shamChangeOpen:
				states[idx] = "open"
			case shamChangeClosed:
				states[idx] = "closed"
			case shamChangeMerged:
				states[idx] = "merged"
			}
			delete(changeNumToIdx, c.Number)

			if len(changeNumToIdx) == 0 {
				break
			}
		}
	}
	sh.mu.RUnlock()

	if len(changeNumToIdx) > 0 {
		return nil, notFoundErrorf("changes not found: %v", changeNumToIdx)
	}

	return &statesResponse{States: states}, nil
}

func (r *forgeRepository) ChangesStates(ctx context.Context, fids []forge.ChangeID) ([]forge.ChangeState, error) {
	ids := make([]ChangeID, len(fids))
	for i, fid := range fids {
		ids[i] = fid.(ChangeID)
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "change", "states")
	req := statesRequest{IDs: ids}

	var res statesResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("get states: %w", err)
	}

	states := make([]forge.ChangeState, len(res.States))
	for i, state := range res.States {
		switch state {
		case "open":
			states[i] = forge.ChangeOpen
		case "closed":
			states[i] = forge.ChangeClosed
		case "merged":
			states[i] = forge.ChangeMerged
		default:
			states[i] = forge.ChangeOpen // default to open for unknown states
		}
	}

	return states, nil
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

	// Squash requests that the CR be merged
	// as a single squashed commit with the PR subject/body
	// instead of a merge commit.
	Squash bool
}

// MergeChange merges an open change against this forge.
func (sh *ShamHub) MergeChange(req MergeChangeRequest) error {
	if req.Owner == "" || req.Repo == "" || req.Number == 0 {
		return errors.New("owner, repo, and number are required")
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

	changeIdx := -1
	var change shamChange
	for idx, c := range sh.changes {
		if c.Base.Owner == req.Owner && c.Base.Repo == req.Repo && c.Number == req.Number {
			changeIdx = idx
			change = c
			break
		}
	}

	if changeIdx == -1 {
		return fmt.Errorf("change %d (%v/%v) not found", req.Number, req.Owner, req.Repo)
	}

	if change.State != shamChangeOpen {
		return fmt.Errorf("change %d (%v/%v) is not open", req.Number, req.Owner, req.Repo)
	}

	// Determine if this is a cross-fork merge by checking if the head branch
	// exists in the target repository or needs to be fetched from a fork
	targetRepoDir := sh.repoDir(req.Owner, req.Repo)

	baseRef := change.Base
	headRef := change.Head

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// If head is in a different repository (fork), fetch it.
	if headRef.Owner != req.Owner || headRef.Repo != req.Repo {
		// Fetch the head branch from the fork
		forkRepoDir := sh.repoDir(headRef.Owner, headRef.Repo)
		if err := xec.Command(ctx, sh.log, sh.gitExe, "fetch", forkRepoDir, headRef.Name+":"+headRef.Name).
			WithDir(targetRepoDir).
			Run(); err != nil {
			return fmt.Errorf("fetch from fork: %w", err)
		}
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
		out, err := xec.Command(ctx, sh.log, sh.gitExe, "merge-tree", "--write-tree", baseRef.Name, headRef.Name).
			WithDir(targetRepoDir).
			Output()
		if err != nil {
			return "", fmt.Errorf("merge-tree: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}()
	if err != nil {
		return err
	}

	commit, err := func() (string, error) {
		change := sh.changes[changeIdx]

		var msg string
		args := []string{"commit-tree", "-p", baseRef.Name}
		if req.Squash {
			msg = fmt.Sprintf("%s (#%d)\n\n%s",
				change.Subject,
				req.Number,
				change.Body)
		} else {
			msg = fmt.Sprintf("Merge change #%d", req.Number)
			args = append(args, "-p", headRef.Name)
		}
		args = append(args, "-m", msg, tree)

		out, err := xec.Command(ctx, sh.log, sh.gitExe, args...).
			WithDir(sh.repoDir(req.Owner, req.Repo)).
			AppendEnv(commitEnv...).
			Output()
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
		ref := "refs/heads/" + sh.changes[changeIdx].Base.Name
		if err := xec.Command(ctx, sh.log, sh.gitExe, "update-ref", ref, commit).
			WithDir(sh.repoDir(req.Owner, req.Repo)).
			Run(); err != nil {
			return fmt.Errorf("update-ref: %w", err)
		}

		return nil
	}()
	if err != nil {
		return err
	}

	if req.DeleteBranch {
		err := func() error {
			if err := xec.Command(ctx, sh.log, sh.gitExe, "branch", "-D", change.Head.Name).
				WithDir(sh.repoDir(change.Head.Owner, change.Head.Repo)).
				Run(); err != nil {
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
