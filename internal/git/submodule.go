package git

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
