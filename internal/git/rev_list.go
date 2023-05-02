package git

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"
)

// ListCommits returns a list of commits in the range [start, stop).
func (r *Repository) ListCommits(ctx context.Context, start, stop string) (iter.Seq2[Hash, error], error) {
	cmd := r.gitCmd(ctx, "rev-list", start, "--not", stop)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(out)
	return func(yield func(Hash, error) bool) {
		var finished bool
		defer func() {
			if finished {
				return
			}

			// If we stopped early, kill the command and consume
			// its output.
			_ = cmd.Kill(r.exec)
			_, _ = io.Copy(io.Discard, out)
		}()

		for scanner.Scan() {
			yield(Hash(scanner.Text()), nil)
		}

		if err := scanner.Err(); err != nil {
			yield(ZeroHash, fmt.Errorf("scan: %w", err))
			return
		}

		if err := cmd.Wait(r.exec); err != nil {
			yield(ZeroHash, fmt.Errorf("wait: %w", err))
			return
		}

		finished = true
	}, nil
}
