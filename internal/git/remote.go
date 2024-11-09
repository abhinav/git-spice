package git

import (
	"bufio"
	"context"
	"fmt"
	"iter"
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

// RemoteRef is a reference in a remote Git repository.
type RemoteRef struct {
	// Name is the full name of the reference.
	// For example "refs/heads/main".
	Name string

	// Hash is the Git object hash that the reference points to.
	Hash Hash
}

// ListRemoteRefsOptions control the behavior of ListRemoteRefs.
type ListRemoteRefsOptions struct {
	// Heads filters the references to only those under refs/heads.
	Heads bool

	// Patterns specifies additional filters on the reference names.
	Patterns []string
}

// ListRemoteRefs lists references in a remote Git repository
// that match the given options.
func (r *Repository) ListRemoteRefs(
	ctx context.Context, remote string, opts *ListRemoteRefsOptions,
) iter.Seq2[RemoteRef, error] {
	if opts == nil {
		opts = &ListRemoteRefsOptions{}
	}

	args := []string{"ls-remote", "--quiet"}
	if opts.Heads {
		args = append(args, "--heads")
	}
	args = append(args, remote)
	args = append(args, opts.Patterns...)

	return func(yield func(RemoteRef, error) bool) {
		cmd := r.gitCmd(ctx, args...)
		out, err := cmd.StdoutPipe()
		if err != nil {
			yield(RemoteRef{}, fmt.Errorf("pipe stdout: %w", err))
			return
		}

		if err := cmd.Start(r.exec); err != nil {
			yield(RemoteRef{}, fmt.Errorf("start: %w", err))
			return
		}
		var finished bool
		defer func() {
			if !finished {
				_ = cmd.Kill(r.exec)
			}
		}()

		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			// Each line is in the form:
			//
			//	<hash> TAB <ref>
			line := scanner.Text()
			oid, ref, ok := strings.Cut(line, "\t")
			if !ok {
				r.log.Warn("Bad ls-remote output", "line", line, "error", "missing a tab")
				continue
			}

			if !yield(RemoteRef{
				Name: ref,
				Hash: Hash(oid),
			}, nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(RemoteRef{}, fmt.Errorf("scan: %w", err))
			return
		}

		if err := cmd.Wait(r.exec); err != nil {
			yield(RemoteRef{}, fmt.Errorf("git ls-remote: %w", err))
			return
		}

		finished = true
	}
}
