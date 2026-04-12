// Package retry provides retry helpers for operations
// that may succeed within a bounded amount of time.
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/must"
)

// Exponential retries an operation with exponentially increasing delays.
type Exponential struct {
	// Delay is the delay before the second attempt.
	// Each later retry doubles the previous delay.
	// Delay must be greater than zero.
	Delay time.Duration

	// Timeout is the total amount of time available
	// for retryable failures after the first attempt.
	// When the timeout is exhausted,
	// Do returns an [ExhaustedError].
	Timeout time.Duration
}

// Attempt describes a single invocation of a retried operation.
type Attempt struct {
	// Number is 1-indexed.
	Number int
}

// ExhaustedError reports that the retry timeout was consumed
// by retryable failures.
type ExhaustedError struct {
	// Attempts is the number of attempts that were made
	// before the retry timeout was exhausted.
	Attempts int

	// Err is the last retryable error returned by the operation.
	Err error
}

// Error implements the error interface.
func (e *ExhaustedError) Error() string {
	return fmt.Sprintf(
		"exhausted retry timeout after %d attempts: %v",
		e.Attempts, e.Err,
	)
}

// Unwrap returns the last retryable error.
func (e *ExhaustedError) Unwrap() error {
	return e.Err
}

type terminalError struct {
	err error
}

// Fail marks err as terminal so retries stop immediately.
func Fail(err error) error {
	must.NotBeNilf(err, "retry.Fail(nil)")
	return &terminalError{err: err}
}

func (e *terminalError) Error() string {
	return e.err.Error()
}

func (e *terminalError) Unwrap() error {
	return e.err
}

// Do invokes fn and retries plain errors with exponential backoff
// until the timeout is exhausted.
//
// A nil error from fn reports success.
// An error produced by [Fail] stops retrying immediately
// and is returned unwrapped.
// Any other error is retried until the timeout is exhausted,
// at which point an [ExhaustedError] is returned.
//
// If ctx is canceled before or during a backoff delay,
// Do returns ctx.Err().
func (e Exponential) Do(
	ctx context.Context,
	fn func(Attempt) error,
) error {
	must.Bef(e.Delay > 0, "retry.Exponential.Delay must be > 0")
	must.Bef(e.Timeout > 0, "retry.Exponential.Timeout must be > 0")

	// Needs to be separate to distinguish between attempts exhausted
	// and underlying timeout being cancelled.
	deadlineCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	var lastErr error
	for attemptNum := 1; ; attemptNum++ {
		err := fn(Attempt{Number: attemptNum})
		if err == nil {
			return nil
		}

		if term, ok := errors.AsType[*terminalError](err); ok {
			return term.err
		}

		delay := e.Delay << (attemptNum - 1)
		lastErr = err
		select {
		case <-time.After(delay):
			// Try again.

		case <-deadlineCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return &ExhaustedError{
				Attempts: attemptNum,
				Err:      lastErr,
			}
		}
	}
}
