package shamhub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/xec"
)

type statesRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	IDs []ChangeID `json:"ids"`
}

type statesResponse struct {
	Statuses []changeStatus `json:"statuses"`
}

var _ = shamhubRESTHandler("POST /{owner}/{repo}/change/states", (*ShamHub).handleStates)

type changeStatus struct {
	State    string `json:"state"`
	HeadHash string `json:"headHash"`
}

func (sh *ShamHub) handleStates(_ context.Context, req *statesRequest) (*statesResponse, error) {
	owner, repo := req.Owner, req.Repo

	changeNumToIdx := make(map[int]int, len(req.IDs))
	for i, id := range req.IDs {
		changeNumToIdx[int(id)] = i
	}

	sh.mu.RLock()
	defer sh.mu.RUnlock()

	statuses := make([]changeStatus, len(changeNumToIdx))
	for _, c := range sh.changes {
		if c.Base.Owner == owner && c.Base.Repo == repo {
			idx, ok := changeNumToIdx[c.Number]
			if !ok {
				continue
			}
			switch c.State {
			case shamChangeOpen:
				statuses[idx].State = "open"
			case shamChangeClosed:
				statuses[idx].State = "closed"
			case shamChangeMerged:
				statuses[idx].State = "merged"
			}
			if c.HeadHash != "" {
				statuses[idx].HeadHash = c.HeadHash
			} else {
				head, err := sh.toChangeBranch(c.Head)
				if err != nil {
					return nil, fmt.Errorf("head branch: %w", err)
				}
				statuses[idx].HeadHash = head.Hash
			}
			delete(changeNumToIdx, c.Number)

			if len(changeNumToIdx) == 0 {
				break
			}
		}
	}
	if len(changeNumToIdx) > 0 {
		return nil, notFoundErrorf("changes not found: %v", changeNumToIdx)
	}

	return &statesResponse{Statuses: statuses}, nil
}

func (r *forgeRepository) ChangeStatuses(ctx context.Context, fids []forge.ChangeID) ([]forge.ChangeStatus, error) {
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

	statuses := make([]forge.ChangeStatus, len(res.Statuses))
	for i, status := range res.Statuses {
		switch status.State {
		case "open":
			statuses[i].State = forge.ChangeOpen
		case "closed":
			statuses[i].State = forge.ChangeClosed
		case "merged":
			statuses[i].State = forge.ChangeMerged
		default:
			statuses[i].State = forge.ChangeOpen // default to open for unknown states
		}
		statuses[i].HeadHash = git.Hash(status.HeadHash)
	}

	return statuses, nil
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

	// MergeMethod controls how the CR is merged.
	// If empty, MergeChange uses the normal merge commit behavior.
	MergeMethod MergeMethod

	// Squash requests that the CR be merged
	// as a single squashed commit with the PR subject/body
	// instead of a merge commit.
	Squash bool

	// HeadHash, if non-empty, causes the merge to fail
	// if the change's head doesn't match this hash.
	HeadHash string
}

// MergeMethod names a server-side merge strategy.
type MergeMethod string

const (
	// MergeMethodMerge creates a two-parent merge commit.
	MergeMethodMerge MergeMethod = "merge"

	// MergeMethodSquash creates a single-parent squashed commit.
	MergeMethodSquash MergeMethod = "squash"
)

func parseMergeMethod(value string) (MergeMethod, error) {
	switch MergeMethod(value) {
	case MergeMethodMerge, MergeMethodSquash:
		return MergeMethod(value), nil
	default:
		return "", fmt.Errorf("unsupported mergeMethod %q", value)
	}
}

func (m MergeMethod) squash() bool {
	return m == MergeMethodSquash
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
	if req.MergeMethod == "" {
		req.MergeMethod = MergeMethodMerge
	}
	if req.Squash {
		req.MergeMethod = MergeMethodSquash
	}
	if _, err := parseMergeMethod(string(req.MergeMethod)); err != nil {
		return err
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

	if req.Time.IsZero() {
		out, err := xec.Command(
			ctx, sh.log, sh.gitExe,
			"log", "-1", "--format=%cI", headRef.Name,
		).
			WithDir(sh.repoDir(headRef.Owner, headRef.Repo)).
			Output()
		if err != nil {
			return fmt.Errorf("read head commit time: %w", err)
		}

		req.Time, err = time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
		if err != nil {
			return fmt.Errorf("parse head commit time: %w", err)
		}
	}

	commitEnv := []string{
		"GIT_COMMITTER_NAME=" + req.CommitterName,
		"GIT_COMMITTER_EMAIL=" + req.CommitterEmail,
		"GIT_AUTHOR_NAME=" + req.CommitterName,
		"GIT_AUTHOR_EMAIL=" + req.CommitterEmail,
		"GIT_COMMITTER_DATE=" + req.Time.Format(time.RFC3339),
		"GIT_AUTHOR_DATE=" + req.Time.Format(time.RFC3339),
	}

	// If a HeadHash is provided, verify the head matches.
	if req.HeadHash != "" {
		headRepoDir := sh.repoDir(headRef.Owner, headRef.Repo)
		out, err := xec.Command(ctx, sh.log, sh.gitExe,
			"rev-parse", headRef.Name).
			WithDir(headRepoDir).
			Output()
		if err != nil {
			return fmt.Errorf("resolve head ref: %w", err)
		}
		actual := strings.TrimSpace(string(out))
		if actual != req.HeadHash {
			return fmt.Errorf(
				"head hash mismatch: expected %s, got %s",
				req.HeadHash, actual,
			)
		}
	}

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
		if req.MergeMethod.squash() {
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

	headHash, err := func() (string, error) {
		out, err := xec.Command(ctx, sh.log, sh.gitExe, "rev-parse", headRef.Name).
			WithDir(sh.repoDir(headRef.Owner, headRef.Repo)).
			Output()
		if err != nil {
			return "", fmt.Errorf("rev-parse head: %w", err)
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
	sh.changes[changeIdx].HeadHash = headHash
	return nil
}

// REST endpoint: merge a change.

type mergeChangeRequest struct {
	Owner       string `path:"owner" json:"-"`
	Repo        string `path:"repo" json:"-"`
	Number      int    `path:"number" json:"-"`
	HeadHash    string `json:"headHash,omitempty"`
	MergeMethod string `json:"mergeMethod,omitempty"`
}

type mergeChangeResponse struct{}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/change/{number}/merge",
	(*ShamHub).handleMergeChange,
)

func (sh *ShamHub) handleMergeChange(
	_ context.Context, req *mergeChangeRequest,
) (*mergeChangeResponse, error) {
	mergeMethod := MergeMethod(req.MergeMethod)
	if mergeMethod == "" {
		sh.mu.RLock()
		mergeMethod = sh.defaultMergeMethod
		sh.mu.RUnlock()
	} else if _, err := parseMergeMethod(string(mergeMethod)); err != nil {
		return nil, badRequestErrorf("%s", err)
	}

	err := sh.MergeChange(MergeChangeRequest{
		Owner:       req.Owner,
		Repo:        req.Repo,
		Number:      req.Number,
		HeadHash:    req.HeadHash,
		MergeMethod: mergeMethod,
	})
	if err != nil {
		return nil, err
	}
	return &mergeChangeResponse{}, nil
}

// MergeChange merges a change via the ShamHub REST API.
func (r *forgeRepository) MergeChange(
	ctx context.Context, fid forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "merge",
	)

	body := mergeChangeRequest{
		HeadHash: string(opts.HeadHash),
	}
	var res mergeChangeResponse
	if err := r.client.Post(ctx, u.String(), body, &res); err != nil {
		return fmt.Errorf("merge change: %w", err)
	}

	return nil
}
