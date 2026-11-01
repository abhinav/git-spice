package git

import (
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
)

// WriteBlobs writes the byte slices as Git blobs
// and yields their hashes in input order as Git acknowledges them.
// Previously yielded hashes remain valid if a later write fails.
// Writing or cleanup failures terminate the iterator with an error.
// Some objects may be written without their hashes being yielded.
// No refs are updated.
//
// Writing starts when the iterator is consumed.
// Stopping iteration early terminates and waits for Git
// and removes the temporary files.
func (r *Repository) WriteBlobs(
	ctx context.Context,
	blobs iter.Seq2[[]byte, error],
) iter.Seq2[Hash, error] {
	return func(yield func(Hash, error) bool) {
		dir, err := os.MkdirTemp("", "git-spice-blobs-")
		if err != nil {
			yield("", fmt.Errorf("create blob spool: %w", err))
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Own both pipe ends: Lines starts lazily, so the producer can fail
		// before Git starts reading its stdin.
		stdinReader, stdin, err := os.Pipe()
		if err != nil {
			yield("", fmt.Errorf("hash-object stdin pipe: %w", err))
			return
		}
		defer func() { _ = stdinReader.Close() }()
		defer func() { _ = stdin.Close() }()

		// A fixed relative filename avoids quoting the temporary directory
		// in the line-delimited protocol while Git still writes to our repo.
		cmd := r.gitCmd(ctx, "hash-object", "-w", "--stdin-paths", "--no-filters").
			WithDir(dir).
			WithStdin(stdinReader).
			AppendEnv(
				"GIT_DIR="+r.gitDir,
				// Flush each hash even with stdout piped: it acknowledges
				// when the temporary file can be reused.
				"GIT_FLUSH=1",
			)

		next, stop := iter.Pull2(cmd.Lines())
		var exhausted bool
		defer func() {
			cancel()
			stop()
			// Lines waits at EOF; an early stop also needs to reap Git.
			if !exhausted {
				_ = cmd.Wait()
			}
		}()

		path := filepath.Join(dir, "blob")
		for body, err := range blobs {
			if err != nil {
				yield("", fmt.Errorf("read blob: %w", err))
				return
			}

			if err := os.WriteFile(path, body, 0o600); err != nil {
				yield("", fmt.Errorf("write blob file: %w", err))
				return
			}

			if _, err := fmt.Fprintln(stdin, "blob"); err != nil {
				yield("", fmt.Errorf("write blob type: %w", err))
				return
			}

			// Wait for Git to consume the file before it can be overwritten.
			hash, err, ok := next()
			if err != nil {
				yield("", fmt.Errorf("read blob hash: %w", err))
				return
			}
			if !ok {
				yield("", fmt.Errorf("read blob hash: %w", io.ErrUnexpectedEOF))
				return
			}

			if !yield(Hash(hash), nil) {
				return
			}
		}
		if err := stdin.Close(); err != nil {
			yield("", fmt.Errorf("close blob stdin: %w", err))
			return
		}

		hash, err, ok := next()
		if err != nil {
			yield("", err)
			return
		}
		if ok {
			yield("", fmt.Errorf("unexpected blob hash %q", hash))
			return
		}

		exhausted = true
	}
}
