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

func TestRunner_Run_shellCommand(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`echo "hello world"`,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Title)
	assert.Empty(t, result.Body)
}

func TestRunner_Run_multiLineOutput(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := `printf "Add feature X\n\nThis adds feature X to the system.\nIt does Y and Z."`
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "Add feature X", result.Title)
	assert.Equal(t,
		"This adds feature X to the system.\nIt does Y and Z.",
		result.Body,
	)
}

func TestRunner_Run_envVars(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`echo "branch=$GS_BRANCH base=$GS_BASE"`,
		t.TempDir(),
		[]string{"GS_BRANCH=feature-x", "GS_BASE=main"},
	)
	require.NoError(t, err)
	assert.Equal(t,
		"branch=feature-x base=main",
		result.Title,
	)
}

func TestRunner_Run_workingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t,
		os.WriteFile(
			filepath.Join(dir, "marker.txt"),
			[]byte("found"),
			0o644,
		),
	)

	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`cat marker.txt`,
		dir,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "found", result.Title)
}

func TestRunner_Run_shebang(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := "#!/bin/sh\necho \"shebang works\""
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "shebang works", result.Title)
}

func TestRunner_Run_shebangWithEnv(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	script := "#!/bin/sh\necho \"$GS_BRANCH\""
	result, err := runner.Run(
		t.Context(),
		script,
		t.TempDir(),
		[]string{"GS_BRANCH=my-branch"},
	)
	require.NoError(t, err)
	assert.Equal(t, "my-branch", result.Title)
}

func TestRunner_Run_gitOptionalLocks(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		`echo "$GIT_OPTIONAL_LOCKS"`,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "0", result.Title)
}

func TestRunner_Run_shebang_gitOptionalLocks(t *testing.T) {
	runner := &Runner{Log: silog.Nop()}
	result, err := runner.Run(
		t.Context(),
		"#!/bin/sh\necho \"$GIT_OPTIONAL_LOCKS\"",
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
	assert.Contains(t, err.Error(), "no output")
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
	assert.Contains(t, err.Error(), "run script")
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

func TestRunner_Run_argsShell(t *testing.T) {
	runner := &Runner{
		Log:  silog.Nop(),
		Args: []string{"gs", "commit", "create", "--fill"},
	}
	result, err := runner.Run(
		t.Context(),
		`echo "$@"`,
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "commit create --fill", result.Title)
}

func TestRunner_Run_argsShebang(t *testing.T) {
	runner := &Runner{
		Log:  silog.Nop(),
		Args: []string{"gs", "branch", "submit", "--fill"},
	}
	result, err := runner.Run(
		t.Context(),
		"#!/bin/sh\necho \"$@\"",
		t.TempDir(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "gs branch submit --fill", result.Title)
}

func TestParseOutput(t *testing.T) {
	tests := []struct {
		name      string
		give      string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "SingleLine",
			give:      "fix: a bug",
			wantTitle: "fix: a bug",
		},
		{
			name:      "TitleAndBody",
			give:      "feat: add X\n\nBody text here.",
			wantTitle: "feat: add X",
			wantBody:  "Body text here.",
		},
		{
			name:      "MultipleBlankLines",
			give:      "title\n\nparagraph 1\n\nparagraph 2",
			wantTitle: "title",
			wantBody:  "paragraph 1\n\nparagraph 2",
		},
		{
			name:      "TrailingWhitespace",
			give:      "  title  \n\n  body  ",
			wantTitle: "title",
			wantBody:  "body",
		},
		{
			name: "ContentBetweenTitleAndBlankLine",
			give: "title\nextra line\n\nbody",
			// Only the first line before
			// the first blank line is the title.
			wantTitle: "title",
			wantBody:  "body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseOutput(tt.give)
			assert.Equal(t, tt.wantTitle, result.Title)
			assert.Equal(t, tt.wantBody, result.Body)
		})
	}
}
