package git

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// OpenFileAt starts reading path from the tree at commit.
//
// The caller must close the returned reader to wait for Git and receive its
// exit status.
func (w *Worktree) OpenFileAt(
	ctx context.Context,
	commit Hash,
	path string,
) (io.ReadCloser, error) {
	cmd := w.gitCmd(ctx, "cat-file", "blob", commit.String()+":"+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(
			fmt.Errorf("start cat-file: %w", err),
			stdout.Close(),
		)
	}

	return &fileAtReader{
		ReadCloser: stdout,
		cmd:        cmd,
	}, nil
}

// fileAtReader waits for Git after its stdout pipe is closed.
type fileAtReader struct {
	io.ReadCloser
	cmd *gitCmd
}

// Close releases the pipe and reports the Git process exit status.
func (r *fileAtReader) Close() error {
	closeErr := r.ReadCloser.Close()
	waitErr := r.cmd.Wait()
	if waitErr != nil {
		waitErr = fmt.Errorf("cat-file: %w", waitErr)
	}
	return errors.Join(closeErr, waitErr)
}
