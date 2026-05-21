package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/handler/integration"
)

func TestLoadResolutionFile_absent(t *testing.T) {
	dir := t.TempDir()

	f, err := integration.LoadResolutionFile(
		filepath.Join(dir, "nonexistent.json"))
	require.NoError(t, err)
	assert.Equal(t, []integration.ResolutionEntry{}, f.Resolutions)
	assert.Nil(t, f.CurrentMerge)
}

func TestLoadResolutionFile_malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := integration.LoadResolutionFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoadResolutionFile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, integration.ResolutionFileName)

	want := &integration.ResolutionFile{
		CurrentMerge: &integration.MergePair{
			Ours:   "preview",
			Theirs: "feat-a",
		},
		Resolutions: []integration.ResolutionEntry{
			{
				MergingBranches: integration.MergePair{
					Ours:   "preview",
					Theirs: "feat-a",
				},
				ResolutionInstructions: []integration.QAPair{
					{Question: "q1", Answer: "a1"},
					{Question: "q2", Answer: "a2"},
				},
			},
		},
	}
	require.NoError(t, want.Save(path))

	got, err := integration.LoadResolutionFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestResolutionFile_Save_atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, integration.ResolutionFileName)

	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{
				MergingBranches: integration.MergePair{
					Ours:   "a",
					Theirs: "b",
				},
				ResolutionInstructions: []integration.QAPair{},
			},
		},
	}
	require.NoError(t, f.Save(path))

	// File exists, no leftover .tmp.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp",
			"unexpected leftover temp file: %s", e.Name())
	}

	// File is parseable JSON.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
}

func TestResolutionFile_EnsureEntry_idempotent(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{},
	}

	p := integration.MergePair{Ours: "x", Theirs: "y"}
	e1 := f.EnsureEntry(p)
	e2 := f.EnsureEntry(p)
	assert.Same(t, e1, e2,
		"second call should return the same entry instance")
	assert.Len(t, f.Resolutions, 1)
}

func TestResolutionFile_EntryFor(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{
				MergingBranches: integration.MergePair{
					Ours:   "preview",
					Theirs: "feat-a",
				},
				ResolutionInstructions: []integration.QAPair{
					{Question: "q", Answer: "a"},
				},
			},
		},
	}

	got := f.EntryFor(integration.MergePair{Ours: "preview", Theirs: "feat-a"})
	require.NotNil(t, got)
	assert.Equal(t, "q", got.ResolutionInstructions[0].Question)

	missing := f.EntryFor(integration.MergePair{
		Ours:   "preview",
		Theirs: "feat-z",
	})
	assert.Nil(t, missing)
}

func TestResolutionFile_AppendInstructions(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{},
	}
	p := integration.MergePair{Ours: "preview", Theirs: "feat-a"}

	f.AppendInstructions(p,
		integration.QAPair{Question: "q1", Answer: "a1"})
	f.AppendInstructions(p,
		integration.QAPair{Question: "q2", Answer: "a2"})

	e := f.EntryFor(p)
	require.NotNil(t, e)
	assert.Len(t, e.ResolutionInstructions, 2)
	assert.Equal(t, "q2", e.ResolutionInstructions[1].Question)
}

func TestResolutionFile_PruneBranch(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{MergingBranches: integration.MergePair{Ours: "preview", Theirs: "feat-a"}},
			{MergingBranches: integration.MergePair{Ours: "preview", Theirs: "feat-b"}},
			{MergingBranches: integration.MergePair{Ours: "other", Theirs: "feat-a"}},
		},
	}

	// feat-a appears as theirs in two entries.
	removed := f.PruneBranch("feat-a")
	assert.Equal(t, 2, removed)
	assert.Len(t, f.Resolutions, 1)
	assert.Equal(t, "feat-b", f.Resolutions[0].MergingBranches.Theirs)
}

func TestResolutionFile_PruneBranch_onOursSide(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{MergingBranches: integration.MergePair{Ours: "preview", Theirs: "feat-a"}},
			{MergingBranches: integration.MergePair{Ours: "other", Theirs: "feat-b"}},
		},
	}

	removed := f.PruneBranch("preview")
	assert.Equal(t, 1, removed)
	assert.Len(t, f.Resolutions, 1)
	assert.Equal(t, "other", f.Resolutions[0].MergingBranches.Ours)
}

func TestResolutionFile_PruneStale(t *testing.T) {
	f := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{MergingBranches: integration.MergePair{Ours: "preview", Theirs: "feat-a"}},
			{MergingBranches: integration.MergePair{Ours: "preview", Theirs: "ghost"}},
			{MergingBranches: integration.MergePair{Ours: "gone", Theirs: "feat-a"}},
		},
	}

	tracked := map[string]struct{}{
		"preview": {},
		"feat-a":  {},
	}
	removed := f.PruneStale(tracked)
	assert.Equal(t, 2, removed)
	require.Len(t, f.Resolutions, 1)
	assert.Equal(t, "feat-a", f.Resolutions[0].MergingBranches.Theirs)
}
