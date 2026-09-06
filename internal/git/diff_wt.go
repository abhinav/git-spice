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
	cmd := w.gitCmd(ctx, "diff", base+"..."+head)
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

	return &branchDiffReader{
		ReadCloser: stdout,
		cmd:        cmd,
	}, nil
}

// branchDiffReader waits for the Git process after closing its stdout pipe.
type branchDiffReader struct {
	io.ReadCloser
	cmd *gitCmd
}

// Close releases the pipe and reports the Git process exit status.
func (r *branchDiffReader) Close() error {
	closeErr := r.ReadCloser.Close()
	waitErr := r.cmd.Wait()
	if waitErr != nil {
		waitErr = fmt.Errorf("diff: %w", waitErr)
	}
	return errors.Join(closeErr, waitErr)
}
