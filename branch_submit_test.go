package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchSubmitCmd_messageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	var cmd branchSubmitCmd
	parser, err := kong.New(&cmd)
	require.NoError(t, err)
	_, err = parser.Parse([]string{"--message-file", path})
	require.NoError(t, err)
	require.NotEmpty(t, cmd.MessageFile)
	assert.Equal(t, path, string(cmd.MessageFile))
}

func TestBranchSubmitCmd_messageFile_conflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	for _, flag := range []string{"--title", "--body"} {
		t.Run(flag, func(t *testing.T) {
			var cmd branchSubmitCmd
			parser, err := kong.New(&cmd)
			require.NoError(t, err)
			_, err = parser.Parse([]string{"--message-file", path, flag, "text"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "can't be used together")
		})
	}
}
