package main

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/stub"
)

func TestVersionFlag(t *testing.T) {
	defer stub.Func(&generateBuildReport, "commithash timestamp")()

	var (
		exitCode int
		stdout   bytes.Buffer
	)

	_ = versionFlag(true).BeforeReset(&kong.Kong{
		Stdout: &stdout,
		Exit: func(code int) {
			exitCode = code
		},
	})
	assert.Zero(t, exitCode)
	assert.Contains(t, stdout.String(), "git-spice "+_version)
	assert.Contains(t, stdout.String(), "(commithash timestamp)")
}
