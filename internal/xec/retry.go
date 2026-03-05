package xec

import (
	"context"
	"time"
)

// RetryPolicy controls retry behavior
// for transient command failures.
type RetryPolicy struct {
	// Match reports whether an error
	// is eligible for retry.
	Match func(error) bool

	// Timeout is the maximum total wall-clock time
	// to spend retrying. 0 disables retry.
	// Negative values retry indefinitely.
	Timeout time.Duration

	// BaseDelay is the initial delay
	// between retry attempts.
	// Each subsequent delay doubles.
	BaseDelay time.Duration

	// nowFunc returns the current time.
	// Defaults to [time.Now] if nil.
	nowFunc func() time.Time
}

func (p *RetryPolicy) now() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

// backoff sleeps for an exponentially increasing duration
// based on the attempt number.
// Returns false if the timeout has been exceeded
// or the context was cancelled.
func (p *RetryPolicy) backoff(
	ctx context.Context,
	attempt int,
	deadline time.Time,
) bool {
	delay := p.BaseDelay << attempt

	// Cap delay to remaining time if timeout is positive.
	if p.Timeout > 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if delay > remaining {
			delay = remaining
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
