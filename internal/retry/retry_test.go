package retry

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExponential_Do_successOnFirstAttempt(t *testing.T) {
	var attempts []Attempt

	err := Exponential{
		Timeout: time.Second,
		Delay:   time.Second,
	}.Do(t.Context(), func(attempt Attempt) error {
		attempts = append(attempts, attempt)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []Attempt{{Number: 1}}, attempts)
}

func TestExponential_Do_zeroTimeoutRunsOnce(t *testing.T) {
	var attempts []Attempt

	err := Exponential{
		Timeout: 0,
	}.Do(t.Context(), func(attempt Attempt) error {
		attempts = append(attempts, attempt)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []Attempt{{Number: 1}}, attempts)
}

func TestExponential_Do_zeroTimeoutReturnsErrorDirectly(t *testing.T) {
	errWant := errors.New("fail once")

	err := Exponential{
		Timeout: 0,
	}.Do(t.Context(), func(Attempt) error {
		return errWant
	})

	require.ErrorIs(t, err, errWant)
}

func TestExponential_Do_zeroTimeoutRetryableFailureIsNotExhausted(t *testing.T) {
	errWant := errors.New("fail once")

	err := Exponential{
		Timeout: 0,
	}.Do(t.Context(), func(Attempt) error {
		return errWant
	})

	require.ErrorIs(t, err, errWant)
	var exhausted *ExhaustedError
	assert.False(t, errors.As(err, &exhausted))
}

func TestExponential_Do_successAfterRetryableFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time

		err := Exponential{
			Timeout: time.Second,
			Delay:   100 * time.Millisecond,
		}.Do(t.Context(), func(Attempt) error {
			times = append(times, time.Now())
			if len(times) < 3 {
				return errors.New("try again")
			}
			return nil
		})

		require.NoError(t, err)
		require.Len(t, times, 3)
		assert.Equal(t, 100*time.Millisecond, times[1].Sub(times[0]))
		assert.Equal(t, 200*time.Millisecond, times[2].Sub(times[1]))
	})
}

func TestExponential_Do_timeoutStartsAfterFirstFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errWant := errors.New("still failing")
		attempts := 0

		err := Exponential{
			Timeout: 150 * time.Millisecond,
			Delay:   100 * time.Millisecond,
		}.Do(t.Context(), func(Attempt) error {
			attempts++
			if attempts == 1 {
				time.Sleep(10 * time.Second)
			}
			return errWant
		})

		var exhaustedErr *ExhaustedError
		require.ErrorAs(t, err, &exhaustedErr)
		assert.Equal(t, 2, exhaustedErr.Attempts)
		assert.Equal(t, 2, attempts)
		assert.ErrorIs(t, err, errWant)
	})
}

func TestExponential_Do_exhaustedError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errWant := errors.New("still failing")

		err := Exponential{
			Timeout: 150 * time.Millisecond,
			Delay:   100 * time.Millisecond,
		}.Do(t.Context(), func(Attempt) error {
			return errWant
		})

		var exhaustedErr *ExhaustedError
		require.ErrorAs(t, err, &exhaustedErr)
		assert.Equal(t, 2, exhaustedErr.Attempts)
		assert.ErrorIs(t, err, errWant)
	})
}

func TestExponential_Do_terminalFailure(t *testing.T) {
	errWant := errors.New("do not retry")
	attempts := 0

	err := Exponential{
		Timeout: time.Second,
		Delay:   time.Second,
	}.Do(t.Context(), func(Attempt) error {
		attempts++
		return Fail(errWant)
	})

	require.ErrorIs(t, err, errWant)
	assert.Equal(t, 1, attempts)
}

func TestExponential_Do_contextCanceledDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() {
			done <- Exponential{
				Timeout: 5 * time.Second,
				Delay:   time.Second,
			}.Do(ctx, func(Attempt) error {
				return errors.New("retry")
			})
		}()

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		synctest.Wait()

		err := <-done
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestExponential_Do_attemptValues(t *testing.T) {
	var attempts []Attempt

	err := Exponential{
		Timeout: time.Second,
		Delay:   time.Millisecond,
	}.Do(t.Context(), func(attempt Attempt) error {
		attempts = append(attempts, attempt)
		if attempt.Number < 4 {
			return errors.New("retry")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []Attempt{
		{Number: 1},
		{Number: 2},
		{Number: 3},
		{Number: 4},
	}, attempts)
}

func TestExponential_Do_zeroDelayPanics(t *testing.T) {
	assert.Panics(t, func() {
		_ = Exponential{
			Timeout: time.Second,
			Delay:   0,
		}.Do(t.Context(), func(Attempt) error {
			return nil
		})
	})
}

func TestExponential_Do_zeroTimeoutDoesNotRequireDelay(t *testing.T) {
	err := Exponential{
		Timeout: 0,
		Delay:   0,
	}.Do(t.Context(), func(Attempt) error {
		return nil
	})

	require.NoError(t, err)
}
