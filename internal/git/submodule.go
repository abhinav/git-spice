package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"go.abhg.dev/gs/internal/xec"
)

// Submodule describes a git submodule
// registered in a repository's .gitmodules.
type Submodule struct {
	// Path is the relative path from the repo root.
	Path string

	// URL is the configured remote URL.
	URL string
}

// Submodules lists all submodules in the worktree.
// Returns an empty slice if no submodules are configured.
func (w *Worktree) Submodules(
	ctx context.Context,
) ([]Submodule, error) {
	out, err := w.gitCmd(ctx,
		"config", "--file", ".gitmodules",
		"--get-regexp", `^submodule\..*\.path$`,
	).OutputChomp()
	if err != nil {
		// No .gitmodules or no submodules configured.
		return nil, nil //nolint:nilerr
	}

	return parseSubmoduleConfig(ctx, w, out)
}

// parseSubmoduleConfig parses `git config --get-regexp` output
// for submodule paths, then looks up each submodule's URL.
func parseSubmoduleConfig(
	ctx context.Context,
	w *Worktree,
	output string,
) ([]Submodule, error) {
	var subs []Submodule
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: submodule.<name>.path <value>
		key, path, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}

		sub, err := submoduleFromConfigKey(ctx, w, key, path)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// submoduleFromConfigKey resolves a single submodule entry
// from its config key (submodule.<name>.path) and path value.
func submoduleFromConfigKey(
	ctx context.Context,
	w *Worktree,
	key, path string,
) (Submodule, error) {
	// Extract name: submodule.<name>.path → <name>
	name := strings.TrimPrefix(key, "submodule.")
	name = strings.TrimSuffix(name, ".path")

	urlKey := "submodule." + name + ".url"
	url, err := w.gitCmd(ctx,
		"config", "--file", ".gitmodules",
		"--get", urlKey,
	).OutputChomp()
	if err != nil {
		url = "" // URL may be missing for local submodules.
	}

	return Submodule{
		Path: strings.TrimSpace(path),
		URL:  strings.TrimSpace(url),
	}, nil
}

// SubmoduleCurrentBranch reports the current branch
// of a submodule at the given relative path.
// Returns [ErrDetachedHead] if the submodule HEAD is detached.
func (w *Worktree) SubmoduleCurrentBranch(
	ctx context.Context, path string,
) (string, error) {
	absPath := filepath.Join(w.rootDir, path)
	name, err := newGitCmd(ctx, w.log, w.exec,
		"branch", "--show-current",
	).WithDir(absPath).OutputChomp()
	if err != nil {
		return "", fmt.Errorf("submodule %s: %w", path, err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrDetachedHead
	}
	return name, nil
}

// SubmoduleWorktree opens a [Worktree] for the submodule
// at the given relative path.
func (w *Worktree) SubmoduleWorktree(
	ctx context.Context, path string,
) (*Worktree, error) {
	absPath := filepath.Join(w.rootDir, path)
	return OpenWorktree(ctx, absPath, OpenOptions{
		Log:  w.log,
		exec: w.exec,
	})
}

// UpdateSubmodulePointer stages an updated commit hash
// for a submodule in the index.
// The caller must commit the change separately.
func (w *Worktree) UpdateSubmodulePointer(
	ctx context.Context, path string, hash Hash,
) error {
	// 160000 is the git file mode for submodule links.
	entry := fmt.Sprintf("160000,%s,%s", hash, path)
	if err := w.gitCmd(ctx,
		"update-index", "--cacheinfo", entry,
	).Run(); err != nil {
		return fmt.Errorf("update submodule %s: %w", path, err)
	}
	return nil
}

// SubmoduleStatus captures the runtime state of a submodule
// relative to its parent repository.
type SubmoduleStatus struct {
	// Path is the relative path from the parent repo root.
	Path string

	// HeadHash is the current HEAD commit of the submodule.
	HeadHash Hash

	// GitlinkHash is the commit hash recorded for this submodule
	// in the parent's HEAD tree.
	GitlinkHash Hash

	// Branch is the current branch name of the submodule.
	// Empty when [SubmoduleStatus.Detached] is true.
	Branch string

	// Detached is true when the submodule is in a detached HEAD state.
	Detached bool

	// HasGsStore is true when the submodule has been initialized
	// with `gs repo init`.
	HasGsStore bool
}

// SubmoduleStatus reports the runtime state of the submodule
// at the given relative path.
func (w *Worktree) SubmoduleStatus(
	ctx context.Context, path string,
) (*SubmoduleStatus, error) {
	status := &SubmoduleStatus{Path: path}

	branch, err := w.SubmoduleCurrentBranch(ctx, path)
	switch {
	case errors.Is(err, ErrDetachedHead):
		status.Detached = true
	case err != nil:
		return nil, fmt.Errorf("current branch: %w", err)
	default:
		status.Branch = branch
	}

	head, err := w.SubmoduleHead(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	status.HeadHash = head

	gitlink, err := w.SubmoduleGitlink(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("gitlink: %w", err)
	}
	status.GitlinkHash = gitlink

	hasStore, err := w.SubmoduleHasGsStore(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("gs store: %w", err)
	}
	status.HasGsStore = hasStore

	return status, nil
}

// SubmoduleHead reports the HEAD commit hash of the submodule
// at the given relative path.
func (w *Worktree) SubmoduleHead(
	ctx context.Context, path string,
) (Hash, error) {
	absPath := filepath.Join(w.rootDir, path)
	out, err := newGitCmd(ctx, w.log, w.exec,
		"rev-parse", "HEAD^{commit}",
	).WithDir(absPath).OutputChomp()
	if err != nil {
		return "", fmt.Errorf("submodule %s: %w", path, err)
	}
	return Hash(strings.TrimSpace(out)), nil
}

// SubmoduleGitlink reports the gitlink commit hash recorded
// for the submodule at the given relative path
// in the parent's HEAD tree.
func (w *Worktree) SubmoduleGitlink(
	ctx context.Context, path string,
) (Hash, error) {
	out, err := w.gitCmd(ctx,
		"ls-tree", "HEAD", "--", path,
	).OutputChomp()
	if err != nil {
		return "", fmt.Errorf("ls-tree %s: %w", path, err)
	}
	// Format: <mode> <type> <hash>\t<path>
	// e.g. "160000 commit abc123\tlibs/core"
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return "", fmt.Errorf(
			"unexpected ls-tree output for %s: %q",
			path, out,
		)
	}
	return Hash(fields[2]), nil
}

// SubmoduleHasGsStore reports whether the submodule
// at the given relative path has been initialized with git-spice
// (i.e., the spice data ref exists).
func (w *Worktree) SubmoduleHasGsStore(
	ctx context.Context, path string,
) (bool, error) {
	absPath := filepath.Join(w.rootDir, path)
	err := newGitCmd(ctx, w.log, w.exec,
		"rev-parse", "--verify", "--quiet", "refs/spice/data",
	).WithDir(absPath).Run()
	if err != nil {
		// rev-parse --verify exits non-zero when the ref is absent.
		return false, nil //nolint:nilerr
	}
	return true, nil
}

// AddUpdate stages updates to all tracked files in the worktree
// (`git add -u`). Untracked files are not affected.
func (w *Worktree) AddUpdate(ctx context.Context) error {
	if err := w.gitCmd(ctx, "add", "-u").Run(); err != nil {
		return fmt.Errorf("git add -u: %w", err)
	}
	return nil
}

// HasStagedChanges reports whether the worktree's index differs
// from HEAD (i.e., there is staged content waiting to be committed).
func (w *Worktree) HasStagedChanges(ctx context.Context) (bool, error) {
	// `git diff --cached --quiet` exits 0 when there are no staged
	// changes, 1 when there are, and other non-zero values on error.
	err := w.gitCmd(ctx,
		"diff", "--cached", "--quiet",
	).Run()
	if err == nil {
		return false, nil
	}
	// Exit code 1 means "differences present" — not an error.
	var exitErr *xec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("git diff --cached: %w", err)
}

// HeadSnapshot captures the HEAD state of a worktree
// at a point in time, suitable for restoration via [Worktree.RestoreHead].
type HeadSnapshot struct {
	// Branch is the name of the branch HEAD was on.
	// Empty when [HeadSnapshot.Detached] is true.
	Branch string

	// Hash is the commit hash HEAD pointed at.
	Hash Hash

	// Detached is true when HEAD was detached.
	Detached bool
}

// SnapshotHead captures the current HEAD state of the worktree.
// It records whether HEAD is attached to a branch or detached,
// and the commit hash at HEAD.
func (w *Worktree) SnapshotHead(ctx context.Context) (*HeadSnapshot, error) {
	snap := &HeadSnapshot{}

	branch, err := w.CurrentBranch(ctx)
	switch {
	case errors.Is(err, ErrDetachedHead):
		snap.Detached = true
	case err != nil:
		return nil, fmt.Errorf("current branch: %w", err)
	default:
		snap.Branch = branch
	}

	head, err := w.Head(ctx)
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	snap.Hash = head

	return snap, nil
}

// RestoreHead returns the worktree to the state captured by snap.
// If snap was on a branch, the branch is checked out.
// If snap was detached, HEAD is detached at the captured hash.
// Working-tree changes are carried per `git checkout`'s normal semantics.
func (w *Worktree) RestoreHead(
	ctx context.Context, snap *HeadSnapshot,
) error {
	if snap.Detached {
		return w.DetachHead(ctx, snap.Hash.String())
	}
	return w.CheckoutBranch(ctx, snap.Branch)
}
