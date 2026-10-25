package git

import (
	"errors"
	"syscall"
)

func isArgListTooLong(err error) bool {
	// CreateProcess reports ERROR_FILENAME_EXCED_RANGE for oversized command lines.
	const errorFilenameExcedRange syscall.Errno = 206
	return errors.Is(err, errorFilenameExcedRange) || errors.Is(err, syscall.E2BIG)
}
