package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// DiffBranch runs git diff between the given base and head
// using triple-dot syntax (base...head).
// Output is written directly to stdout and stderr,
// allowing the user to see the diff in their terminal.
func (w *Worktree) DiffBranch(ctx context.Context, base, head string) error {
	if err := w.gitCmd(ctx, "diff", base+"..."+head).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		Run(); err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	return nil
}

// OpenBranchDiff starts a unified diff between base and head using triple-dot
// syntax.
//
// The caller must close the returned reader to wait for Git and receive its
// exit status.
func (w *Worktree) OpenBranchDiff(
	ctx context.Context,
	base, head string,
) (io.ReadCloser, error) {
	return w.openDiff(ctx, base+"..."+head)
}

// OpenCommitDiff starts a unified diff that maps the first commit's tree to
// the second commit's tree. Rename detection is enabled and hunk context is
// omitted because callers use the result to map changed lines.
//
// The caller must close the returned reader to wait for Git and receive its
// exit status.
func (w *Worktree) OpenCommitDiff(
	ctx context.Context,
	from, to string,
) (io.ReadCloser, error) {
	return w.openDiff(
		ctx,
		"--find-renames",
		"--unified=0",
		from,
		to,
	)
}

func (w *Worktree) openDiff(
	ctx context.Context,
	args ...string,
) (io.ReadCloser, error) {
	cmd := w.gitCmd(ctx, "diff", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(
			fmt.Errorf("start diff: %w", err),
			stdout.Close(),
		)
	}

	return &diffReader{
		ReadCloser: stdout,
		cmd:        cmd,
	}, nil
}

// diffReader waits for the Git process after closing its stdout pipe.
type diffReader struct {
	io.ReadCloser
	cmd *gitCmd
}

// Close releases the pipe and reports the Git process exit status.
func (r *diffReader) Close() error {
	closeErr := r.ReadCloser.Close()
	waitErr := r.cmd.Wait()
	if waitErr != nil {
		waitErr = fmt.Errorf("diff: %w", waitErr)
	}
	return errors.Join(closeErr, waitErr)
}
