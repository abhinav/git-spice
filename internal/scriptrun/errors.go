package scriptrun

import (
	"errors"
	"fmt"
)

// ErrEmptyOutput is returned by [ParseResponse] when the script
// produced no stdout.
var ErrEmptyOutput = errors.New("script produced no output")

// InvalidOutputError wraps a JSON parse failure on script output.
// The original output is retained (truncated by callers as needed
// for logging) so handlers can surface diagnostic context.
type InvalidOutputError struct {
	Err    error
	Output []byte
}

func (e *InvalidOutputError) Error() string {
	return fmt.Sprintf("invalid script output: %v", e.Err)
}

func (e *InvalidOutputError) Unwrap() error {
	return e.Err
}
