package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/spicedir"
)

// fakeRunner is a minimal in-memory ScriptRunner used by resolver tests.
type fakeRunner struct {
	lastReq *scriptrun.RunRequest
	result  *scriptrun.RunResult
	err     error
}

func (f *fakeRunner) Run(_ context.Context, req *scriptrun.RunRequest) (*scriptrun.RunResult, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestScriptResolver_Resolve_emptyResponse(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(`{}`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `echo '{}'`,
		Runner:   runner,
		RepoRoot: dir,
	}

	resp, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Assumptions)
	assert.Empty(t, resp.Questions)
	assert.Empty(t, resp.UnresolvedFiles)
}

func TestScriptResolver_Resolve_populatedResponse(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout: []byte(`{
				"assumptions": ["chose feat-a per commit timestamp"],
				"questions": ["should feat-a win in shared.txt?"],
				"unresolved_files": ["shared.txt"]
			}`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `# unused with fake runner`,
		Runner:   runner,
		RepoRoot: dir,
	}

	resp, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"chose feat-a per commit timestamp"}, resp.Assumptions)
	assert.Equal(t,
		[]string{"should feat-a win in shared.txt?"}, resp.Questions)
	assert.Equal(t, []string{"shared.txt"}, resp.UnresolvedFiles)
}

func TestScriptResolver_Resolve_currentMergeWritten(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(`{}`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `echo '{}'`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.NoError(t, err)

	file, err := integration.LoadResolutionFile(
		spicedir.ResolutionPath(dir, integration.ResolutionFeatureName))
	require.NoError(t, err)
	require.NotNil(t, file.CurrentMerge)
	assert.Equal(t, "preview", file.CurrentMerge.Ours)
	assert.Equal(t, "feat-a", file.CurrentMerge.Theirs)
	// EnsureEntry should have created an entry for this pair.
	require.Len(t, file.Resolutions, 1)
	assert.Equal(t, "preview", file.Resolutions[0].MergingBranches.Ours)
	assert.Equal(t, "feat-a", file.Resolutions[0].MergingBranches.Theirs)

	// Verify the script saw a Dir set to the repo root.
	assert.Equal(t, dir, runner.lastReq.Dir)
}

func TestScriptResolver_Resolve_existingEntryPreserved(t *testing.T) {
	dir := t.TempDir()
	path := spicedir.ResolutionPath(dir, integration.ResolutionFeatureName)

	// Pre-seed file with a Q&A entry for (preview, feat-a).
	seed := &integration.ResolutionFile{
		Resolutions: []integration.ResolutionEntry{
			{
				MergingBranches: integration.MergePair{
					Ours: "preview", Theirs: "feat-a",
				},
				ResolutionInstructions: []scriptrun.QAPair{
					{Question: "prior Q", Answer: "prior A"},
				},
			},
		},
	}
	require.NoError(t, seed.Save(path))

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(`{}`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `echo '{}'`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.NoError(t, err)

	file, err := integration.LoadResolutionFile(path)
	require.NoError(t, err)
	require.Len(t, file.Resolutions, 1)
	require.Len(t, file.Resolutions[0].ResolutionInstructions, 1)
	assert.Equal(t, "prior Q",
		file.Resolutions[0].ResolutionInstructions[0].Question)
}

func TestScriptResolver_Resolve_invalidJSON(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(`not-json`),
			Stderr:   []byte(`some debugging info`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `echo not-json`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.Error(t, err)

	var sre *integration.ScriptResolveError
	require.True(t, errors.As(err, &sre))
	assert.Equal(t, "parse", sre.Stage)
	assert.Equal(t, []byte(`not-json`), sre.Stdout)
	assert.Equal(t, []byte(`some debugging info`), sre.Stderr)
}

func TestScriptResolver_Resolve_emptyOutput(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(``),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `:`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.Error(t, err)
	var sre *integration.ScriptResolveError
	require.True(t, errors.As(err, &sre))
	assert.Equal(t, "parse", sre.Stage)
}

func TestScriptResolver_Resolve_nonZeroExit(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 7,
			Stdout:   []byte(`partial stdout`),
			Stderr:   []byte(`error trace`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `exit 7`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.Error(t, err)

	var sre *integration.ScriptResolveError
	require.True(t, errors.As(err, &sre))
	assert.Equal(t, "exit", sre.Stage)
	assert.Equal(t, 7, sre.ExitCode)
	assert.Equal(t, []byte(`partial stdout`), sre.Stdout)
	assert.Equal(t, []byte(`error trace`), sre.Stderr)
}

func TestScriptResolver_Resolve_runnerError(t *testing.T) {
	dir := t.TempDir()

	runner := &fakeRunner{
		err: errors.New("could not spawn"),
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `:`,
		Runner:   runner,
		RepoRoot: dir,
	}

	_, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not spawn")
}

func TestScriptResolver_Resolve_unknownFieldIgnored(t *testing.T) {
	dir := t.TempDir()

	// Per the shared script protocol (doc/src/guide/scripts.md),
	// extra fields are ignored. A document with only an unknown
	// field parses as an empty response.
	runner := &fakeRunner{
		result: &scriptrun.RunResult{
			ExitCode: 0,
			Stdout:   []byte(`{"made_up_field": 1}`),
		},
	}
	r := &integration.ScriptResolver{
		Log:      silog.Nop(),
		Script:   `echo '{"made_up_field":1}'`,
		Runner:   runner,
		RepoRoot: dir,
	}

	resp, err := r.Resolve(t.Context(), &integration.ResolveRequest{
		IntegrationName: "preview",
		TipName:         "feat-a",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Assumptions)
	assert.Empty(t, resp.Questions)
	assert.Empty(t, resp.UnresolvedFiles)
}
