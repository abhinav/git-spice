package git

import (
	"context"
	"errors"

	"go.abhg.dev/gs/internal/retry"
	"go.abhg.dev/gs/internal/xec"
)

// runGitWithIndexLockRetry runs a Git command under the worktree's
// configured index-lock retry policy.
//
// build must construct a fresh command for each attempt.
// This allows callers to rebuild all command state,
// including stdio wiring, transient config, and output buffers,
// without needing cloning support from xec.
//
// If retry is disabled, build is invoked exactly once
// and the resulting command is run once.
// Otherwise, a fresh command is built and run on each attempt
// until it succeeds, fails terminally,
// or the configured timeout is exhausted.
func (w *Worktree) runGitWithIndexLockRetry(
	ctx context.Context,
	build func() *gitCmd,
) error {
	runAttempt := func(attempt retry.Attempt) error {
		cmd := build()
		observer := cmd.ObserveIndexLock()
		if err := cmd.Run(); err != nil {
			if observer.IsIndexLockErr(err) {
				cmd.log.Debug("Retrying Git command after index.lock contention",
					"attempt", attempt.Number,
					"error", err,
				)
				return err
			}
			return retry.Fail(err)
		}
		return nil
	}

	return retry.Exponential{
		Timeout: w.indexLockTimeout,
		Delay:   _indexLockRetryDelay,
	}.Do(ctx, runAttempt)
}

// ObserveIndexLock attaches an index lock observer
// to the command's stderr stream.
//
// The observer sees the same bytes that would otherwise
// go only to the command's current stderr destination.
func (c *gitCmd) ObserveIndexLock() *indexLockObserver {
	observer := new(indexLockObserver)
	c.cmd.TeeStderr(observer)
	return observer
}

const _indexLockToken = "index.lock"

// indexLockObserver watches a byte stream
// for Git's index lock conflict marker.
//
// Git writes "index.lock" in its stderr output
// when it cannot acquire the index lock.
// This observer matches that token incrementally
// as bytes arrive from stderr.
//
// The zero value is ready to use.
// It starts with no partial match state
// and reports that no token has been seen.
type indexLockObserver struct {
	// state is the length of the token prefix
	// matched so far at the end of the stream.
	// It is always in [0, len(_indexLockToken)].
	state int

	// seen reports whether "index.lock"
	// has been observed anywhere in the stream.
	// Once set, it stays set.
	seen bool
}

// Write consumes stderr bytes
// and updates the observer's match state.
func (o *indexLockObserver) Write(p []byte) (int, error) {
	for _, b := range p {
		o.writeByte(b)
	}
	return len(p), nil
}

// Seen reports whether the observer has matched "index.lock".
func (o *indexLockObserver) Seen() bool {
	return o.seen
}

// IsIndexLockErr reports whether err is a non-zero-exit error
// from a command whose stderr stream contained "index.lock".
func (o *indexLockObserver) IsIndexLockErr(err error) bool {
	var exitErr *xec.ExitError
	return errors.As(err, &exitErr) && o.seen
}

// writeByte advances the streaming matcher by one byte.
//
// This is a simple prefix-state matcher over "index.lock".
// The token has no useful repeated prefix structure,
// so on mismatch we only need to either:
//   - restart from state 1 if this byte can begin a fresh match, or
//   - reset to state 0 otherwise.
//
// That makes matching cheap while still handling cases
// where the token is split across arbitrary write boundaries.
func (o *indexLockObserver) writeByte(b byte) {
	if o.seen {
		return
	}

	switch b {
	// Match attempt starts anytime we see 'i'.
	case _indexLockToken[0]:
		o.state = 1

	// o.state moves only if there's a match
	// so it's guaranteed to stay in bounds of the token.
	// So as long as there's a match, keep moving forward.
	case _indexLockToken[o.state]:
		o.state++

	// Anything else resets.
	default:
		o.state = 0
	}

	// Latch the match so later bytes cannot clear it.
	// So if there's text after 'index.lock', we still report a match.
	if o.state == len(_indexLockToken) {
		o.seen = true
		o.state = 0
	}
}
