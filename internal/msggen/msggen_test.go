package msggen

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

// jsonEcho is a small helper that prints a JSON ResolveResponse with
// the given title.
func jsonEcho(title string) string {
	return `printf '{"title":"` + title + `"}'`
}

func TestRunner_Run_shellCommand(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		jsonEcho("hello world"),
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Title)
	assert.Empty(t, result.Body)
}

func TestRunner_Run_titleAndBody(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := `cat <<'EOF'
{"title":"Add feature X","body":"This adds feature X.\nIt does Y and Z."}
EOF`
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "Add feature X", result.Title)
	assert.Equal(t, "This adds feature X.\nIt does Y and Z.", result.Body)
}

func TestRunner_Run_envVars(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := `printf '{"title":"branch=%s base=%s"}' "$GS_BRANCH" "$GS_BASE"`
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		[]string{"GS_BRANCH=feature-x", "GS_BASE=main"},
	)
	require.NoError(t, err)
	assert.Equal(t, "branch=feature-x base=main", result.Title)
}

func TestRunner_Run_workingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "marker.txt"),
		[]byte("found"),
		0o644,
	))

	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`printf '{"title":"%s"}' "$(cat marker.txt)"`,
		dir,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "found", result.Title)
}

func TestRunner_Run_shebang(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := `#!/bin/sh
printf '{"title":"shebang works"}'`
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "shebang works", result.Title)
}

func TestRunner_Run_gitOptionalLocks(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`printf '{"title":"%s"}' "$GIT_OPTIONAL_LOCKS"`,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "0", result.Title)
}

func TestRunner_Run_emptyOutput(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(
		t.Context(),
		`printf ""`,
		t.TempDir(),
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestRunner_Run_missingTitle(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(
		t.Context(),
		`printf '{"body":"only body"}'`,
		t.TempDir(),
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no title")
}

func TestRunner_Run_invalidJSON(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(
		t.Context(),
		`printf 'not json'`,
		t.TempDir(),
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestRunner_Run_assumptionsAndQuestions(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := `printf '{"title":"T","assumptions":["a1","a2"],"questions":["q1"]}'`
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "T", result.Title)
	assert.Equal(t, []string{"a1", "a2"}, result.Assumptions)
	assert.Equal(t, []string{"q1"}, result.Questions)
}

func TestRunner_Run_nonZeroExit(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(
		t.Context(),
		`exit 1`,
		t.TempDir(),
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 1")
}

func TestRunner_Run_contextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(
		ctx,
		`sleep 10`,
		t.TempDir(),
		nil,
	)
	require.Error(t, err)
}

func TestResult_Message(t *testing.T) {
	tests := []struct {
		name string
		give Result
		want string
	}{
		{
			name: "TitleOnly",
			give: Result{Title: "fix: resolve crash"},
			want: "fix: resolve crash",
		},
		{
			name: "TitleAndBody",
			give: Result{
				Title: "feat: add login",
				Body:  "Adds OAuth-based login.",
			},
			want: "feat: add login\n\nAdds OAuth-based login.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.Message())
		})
	}
}

func TestRunner_Run_emptyScript(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	_, err := runner.Run(t.Context(), "", t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty script")
}
