package git

import (
	"bufio"
	"context"
	"fmt"
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
