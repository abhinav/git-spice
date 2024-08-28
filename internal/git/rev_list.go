package git

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListCommits returns a list of commits matched by the given range.
func (r *Repository) ListCommits(ctx context.Context, commits CommitRange) ([]Hash, error) {
	lines, err := r.listCommitsFormat(ctx, commits, "")
	if err != nil {
		return nil, err
	}

	hashes := make([]Hash, len(lines))
	for i, line := range lines {
		hashes[i] = Hash(line)
	}

	return hashes, nil
}

// CommitDetail contains information about a commit.
type CommitDetail struct {
	// Hash is the full hash of the commit.
	Hash Hash

	// ShortHash is the short (usually 7-character) hash of the commit.
	ShortHash Hash

	// Subject is the first line of the commit message.
	Subject string

	// AuthorDate is the time the commit was authored.
	AuthorDate time.Time
}

func (cd *CommitDetail) String() string {
	return fmt.Sprintf("%s %s %s", cd.ShortHash, cd.AuthorDate, cd.Subject)
}

// ListCommitsDetails returns details about commits matched by the given range.
func (r *Repository) ListCommitsDetails(ctx context.Context, commits CommitRange) ([]CommitDetail, error) {
	lines, err := r.listCommitsFormat(ctx, commits, "%H %h %at %s")
	if err != nil {
		return nil, err
	}

	details := make([]CommitDetail, len(lines))
	for i, line := range lines {
		hash, line, ok := strings.Cut(line, " ")
		if !ok {
			r.log.Warn("Bad rev-list output", "line", line, "error", "missing a hash")
			continue
		}

		shortHash, line, ok := strings.Cut(line, " ")
		if !ok {
			r.log.Warn("Bad rev-list output", "line", line, "error", "missing a short hash")
			continue
		}

		epochstr, subject, ok := strings.Cut(line, " ")
		if !ok {
			r.log.Warn("Bad rev-list output", "line", line, "error", "missing an time")
			continue
		}
		epoch, err := strconv.ParseInt(epochstr, 10, 64)
		if err != nil {
			r.log.Warn("Bad rev-list output", "line", line, "error", err)
			continue
		}

		details[i] = CommitDetail{
			Hash:       Hash(hash),
			ShortHash:  Hash(shortHash),
			Subject:    subject,
			AuthorDate: time.Unix(epoch, 0),
		}
	}

	return details, nil
}

// ListCommitsFormat lists commits matched by the given range,
// formatted according to the given format string.
//
// See git-log(1) for details on the format string.
func (r *Repository) listCommitsFormat(ctx context.Context, commits CommitRange, format string) ([]string, error) {
	args := make([]string, 0, len(commits)+3)
	args = append(args, "rev-list")
	if format != "" {
		args = append(args, "--format="+format)
	}
	args = append(args, []string(commits)...)

	cmd := r.gitCmd(ctx, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start rev-list: %w", err)
	}

	// TODO: Return a string iterator
	var lines []string
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := scanner.Text()

		// With --format, rev-list output is in the form:
		//
		//    commit <hash>
		//    <formatted message>
		//
		// We'll need to ignore the first line.
		//
		// This is a bit of a hack, but the --no-commit-header flag
		// that suppresses this line is only available in git 2.33+.
		if format != "" && strings.HasPrefix(line, "commit ") {
			if !scanner.Scan() {
				break
			}
		}

		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("rev-list: %w", err)
	}

	return lines, nil
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

// FirstParent indicates that only the first parent of each commit
// should be listed if it is a merge commit.
func (r CommitRange) FirstParent() CommitRange {
	return append(r, "--first-parent")
}

// Reverse indicates that the commits should be listed in reverse order.
func (r CommitRange) Reverse() CommitRange {
	return append(r, "--reverse")
}
