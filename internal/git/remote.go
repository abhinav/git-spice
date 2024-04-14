package git

import (
	"context"
	"fmt"
	"strings"
)

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
