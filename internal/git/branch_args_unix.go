//go:build linux || darwin

package git

import (
	"errors"
	"syscall"
)

func isArgListTooLong(err error) bool {
	return errors.Is(err, syscall.E2BIG)
}
