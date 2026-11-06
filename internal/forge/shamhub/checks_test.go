package shamhub

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestShamHub_SetChangeCheck(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 1,
				Base: &shamBranch{
					Owner: "alice",
					Repo:  "example",
				},
			},
		},
	}

	require.NoError(t, sh.SetChangeCheck(
		"alice",
		"example",
		1,
		forge.ChangeCheck{
			Name:  "unit tests",
			State: forge.ChangeCheckPending,
		},
	))
	require.NoError(t, sh.SetChangeCheck(
		"alice",
		"example",
		1,
		forge.ChangeCheck{
			Name:  "lint",
			State: forge.ChangeCheckPassed,
		},
	))
	require.NoError(t, sh.SetChangeCheck(
		"alice",
		"example",
		1,
		forge.ChangeCheck{
			Name:  "unit tests",
			State: forge.ChangeCheckFailed,
		},
	))

	checks, err := sh.ChangeChecks("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeCheck{
		{
			Name:  "unit tests",
			State: forge.ChangeCheckFailed,
		},
		{
			Name:  "lint",
			State: forge.ChangeCheckPassed,
		},
	}, checks)
}
