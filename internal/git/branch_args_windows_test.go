package git

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsArgListTooLong_windows(t *testing.T) {
	err := fmt.Errorf("start Git: %w", &os.PathError{
		Op: "fork/exec", Path: "git.exe", Err: syscall.Errno(206),
	})
	assert.True(t, isArgListTooLong(err))
	assert.False(t, isArgListTooLong(os.ErrPermission))
}
