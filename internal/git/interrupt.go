package git

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/xec"
)

// InterruptError reports a Git operation, such as a rebase or merge,
// that was interrupted and left in progress.
type InterruptError interface {
	error

	// InterruptedBranch reports the branch of the interrupted operation.
	InterruptedBranch() string

	interruptError()
}

// handleInterruptedOp converts the error of a failed Git command
// into the InterruptError built by wrap
// if readState finds the operation still in progress.
// suppressStateErrLog, if non-nil, reports readState errors not worth logging.
func handleInterruptedOp[S any](
	ctx context.Context,
	w *Worktree,
	verb string,
	err error,
	readState func(context.Context) (S, error),
	suppressStateErrLog func(error) bool,
	wrap func(S, error) error,
) error {
	if exitErr := new(xec.ExitError); !errors.As(err, &exitErr) {
		return fmt.Errorf("%s: %w", verb, err)
	}

	// The command ran but failed, so the operation may still be in progress.
	state, stateErr := readState(ctx)
	if stateErr != nil {
		if suppressStateErrLog == nil || !suppressStateErrLog(stateErr) {
			w.log.Debug("Failed to read "+verb+" state", "error", stateErr)
		}
		return err
	}

	return wrap(state, err)
}
