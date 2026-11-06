package main

import (
	"context"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.abhg.dev/gs/internal/git"
)

func TestMergeCommandBranchFlag_acceptsRepeatedValues(t *testing.T) {
	t.Run("BranchMerge", func(t *testing.T) {
		var cmd branchMergeCmd
		parser, err := newMergeCommandParser(t, &cmd)
		require.NoError(t, err)

		_, err = parser.Parse([]string{
			"--branch", "feat1",
			"--branch", "feat2",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"feat1", "feat2"}, cmd.Branches)
	})

	t.Run("DownstackMerge", func(t *testing.T) {
		var cmd downstackMergeCmd
		parser, err := newMergeCommandParser(t, &cmd)
		require.NoError(t, err)

		_, err = parser.Parse([]string{
			"--branch", "feat1",
			"--branch", "feat2",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"feat1", "feat2"}, cmd.Branches)
	})

	t.Run("StackMerge", func(t *testing.T) {
		var cmd stackMergeCmd
		parser, err := newMergeCommandParser(t, &cmd)
		require.NoError(t, err)

		_, err = parser.Parse([]string{
			"--branch", "feat1",
			"--branch", "feat2",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"feat1", "feat2"}, cmd.Branches)
	})
}

func TestMergeCommandBranchFlag_acceptsCommaSeparatedValues(t *testing.T) {
	var cmd branchMergeCmd
	parser, err := newMergeCommandParser(t, &cmd)
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"--branch", "feat1,feat2",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"feat1", "feat2"}, cmd.Branches)
}

func newMergeCommandParser(t *testing.T, cmd any) (*kong.Kong, error) {
	t.Helper()

	return kong.New(cmd,
		kong.BindTo(t.Context(), (*context.Context)(nil)),
		kong.Bind((*git.Worktree)(nil)),
	)
}
