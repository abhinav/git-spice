package git

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
)

// ListCommits returns a list of commits matched by the given range.
func (r *Repository) ListCommits(ctx context.Context, commits CommitRange) ([]Hash, error) {
	args := make([]string, 0, len(commits)+1)
	args = append(args, "rev-list")
	args = append(args, []string(commits)...)

	cmd := r.gitCmd(ctx, args...)
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

// CountCommits reports the number of commits matched by the given range.
func (r *Repository) CountCommits(ctx context.Context, commits CommitRange) (int, error) {
	args := make([]string, 0, len(commits)+1)
	args = append(args, "rev-list")
	args = append(args, []string(commits)...)
	args = append(args, "--count")

	cmd := r.gitCmd(ctx, args...)
	out, err := cmd.OutputString(r.exec)
	if err != nil {
		return 0, fmt.Errorf("rev-list: %w", err)
	}

	count, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("rev-list --count: bad output %q: %w", out, err)
	}

	return count, nil
}

// CommitRange builds up arguments for a ListCommits command.
type CommitRange []string

// CommitRangeFrom builds a commit range that reports the given commit
// and all its parents until the root commit.
func CommitRangeFrom(from Hash) CommitRange {
	return CommitRange{string(from)}
}

// ExcludeFrom indicates that the listing should exclude
// commits reachable from the given hash.
func (r CommitRange) ExcludeFrom(hash Hash) CommitRange {
	return append(r, "--not", string(hash))
}

// Limit sets the maximum number of commits to list.
func (r CommitRange) Limit(n int) CommitRange {
	return append(r, "-n", strconv.Itoa(n))
}
