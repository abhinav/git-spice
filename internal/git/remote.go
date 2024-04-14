package git

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

// ListRemotes returns a list of remotes for the repository.
func (r *Repository) ListRemotes(ctx context.Context) ([]string, error) {
	cmd := newGitCmd(ctx, r.log, "remote")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe stdout: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	var remotes []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		remotes = append(remotes, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("git remote: %w", err)
	}

	return remotes, nil
}

// RemoteURL reports the URL of a known Git remote.
func (r *Repository) RemoteURL(ctx context.Context, remote string) (string, error) {
	url, err := r.gitCmd(ctx, "remote", "get-url", remote).OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("remote get-url: %w", err)
	}
	return url, nil
}

// RemoteDefaultBranch reports the default branch of a remote.
// The remote must be known to the repository.
func (r *Repository) RemoteDefaultBranch(ctx context.Context, remote string) (string, error) {
	ref, err := r.gitCmd(
		ctx, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD").
		OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("symbolic-ref: %w", err)
	}

	ref = strings.TrimPrefix(ref, remote+"/")
	return ref, nil
}
