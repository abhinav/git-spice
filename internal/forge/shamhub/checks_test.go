package shamhub

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestShamHub_SetChangeChecks_seedsFullStructure(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 1,
				Base:   &shamBranch{Owner: "alice", Repo: "example"},
			},
		},
	}

	require.NoError(t, sh.SetChangeChecks("alice", "example", 1, &forge.ChangeChecks{
		Rollup: forge.ChecksFailed,
		Runs: []forge.CheckRun{
			{Name: "unit", State: "success", URL: "https://example.test/checks/unit"},
			{Name: "lint", State: "failure", URL: "https://example.test/checks/lint"},
		},
		URL: "https://example.test/checks",
	}))

	checks, err := sh.ChecksByChange("alice", "example", []int{1})
	require.NoError(t, err)
	require.Len(t, checks, 1)

	got := checks[0]
	require.NotNil(t, got)
	assert.Equal(t, forge.ChecksFailed, got.Rollup)
	assert.Equal(t, "https://example.test/checks", got.URL)
	assert.Equal(t, []forge.CheckRun{
		{Name: "unit", State: "success", URL: "https://example.test/checks/unit"},
		{Name: "lint", State: "failure", URL: "https://example.test/checks/lint"},
	}, got.Runs)
}

func TestShamHub_ChecksByChange_rollupOnlyFallback(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number:      7,
				Base:        &shamBranch{Owner: "alice", Repo: "example"},
				ChecksState: forge.ChecksPassed,
			},
		},
	}

	checks, err := sh.ChecksByChange("alice", "example", []int{7})
	require.NoError(t, err)
	require.Len(t, checks, 1)

	got := checks[0]
	require.NotNil(t, got)
	assert.Equal(t, forge.ChecksPassed, got.Rollup)
	assert.Empty(t, got.Runs)
	assert.Empty(t, got.URL)
}

func TestShamHub_ChecksByChange_unsetIsNone(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 3,
				Base:   &shamBranch{Owner: "alice", Repo: "example"},
			},
		},
	}

	checks, err := sh.ChecksByChange("alice", "example", []int{3})
	require.NoError(t, err)
	require.Len(t, checks, 1)
	require.NotNil(t, checks[0])
	assert.Equal(t, forge.ChecksNone, checks[0].Rollup)
}

func TestShamHub_ChecksByChange_unknownChangeIsNilSlot(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 1,
				Base:   &shamBranch{Owner: "alice", Repo: "example"},
			},
		},
	}

	checks, err := sh.ChecksByChange("alice", "example", []int{1, 99})
	require.NoError(t, err)
	require.Len(t, checks, 2)

	assert.NotNil(t, checks[0])
	assert.Nil(t, checks[1])
}

func TestShamHub_handleChecksByChange_wireRoundTrip(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number:      1,
				Base:        &shamBranch{Owner: "alice", Repo: "example"},
				ChecksState: forge.ChecksFailed,
				ChecksRuns: []forge.CheckRun{
					{Name: "unit", State: "neutral", URL: "https://example.test/u"},
				},
				ChecksURL: "https://example.test/checks",
			},
			{
				Number: 2,
				Base:   &shamBranch{Owner: "alice", Repo: "example"},
			},
		},
	}

	res, err := sh.handleChecksByChange(t.Context(), &checksByChangeRequest{
		Owner: "alice",
		Repo:  "example",
		IDs:   []ChangeID{1, 2, 99},
	})
	require.NoError(t, err)
	require.Len(t, res.Checks, 3)

	require.NotNil(t, res.Checks[0])
	assert.Equal(t, "failed", res.Checks[0].Rollup)
	assert.Equal(t, "https://example.test/checks", res.Checks[0].URL)
	require.Len(t, res.Checks[0].Runs, 1)
	assert.Equal(t, "unit", res.Checks[0].Runs[0].Name)
	assert.Equal(t, "neutral", res.Checks[0].Runs[0].State)
	assert.Equal(t, "https://example.test/u", res.Checks[0].Runs[0].URL)

	require.NotNil(t, res.Checks[1])
	assert.Equal(t, "none", res.Checks[1].Rollup)
	assert.Empty(t, res.Checks[1].Runs)
	assert.Empty(t, res.Checks[1].URL)

	assert.Nil(t, res.Checks[2])
}
