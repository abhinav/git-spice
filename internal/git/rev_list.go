package git

import (
	"bufio"
	"context"
	"fmt"
)

// ListCommits returns a list of commits in the range [start, stop).
func (r *Repository) ListCommits(ctx context.Context, start, stop string) ([]Hash, error) {
	cmd := r.gitCmd(ctx, "rev-list", start, "--not", stop)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start rev-list: %w", err)
	}

	var revs []Hash
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		revs = append(revs, Hash(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("rev-list: %w", err)
	}

	return revs, nil
}
