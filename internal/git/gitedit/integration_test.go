package gitedit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

// editorConfig holds editor-related git configuration
// loaded from a real repository.
type editorConfig struct {
	CommentString string // core.commentChar or core.commentString
	CleanupMode   string // commit.cleanup
	Verbose       bool   // commit.verbose
}

// loadEditorConfig reads editor-related configuration
// from the git repository at repoDir.
func loadEditorConfig(
	t *testing.T,
	repoDir string,
) editorConfig {
	t.Helper()

	cfg := git.NewConfig(git.ConfigOptions{
		Dir: repoDir,
		Log: silogtest.New(t),
	})

	var ec editorConfig
	for entry, err := range cfg.ListRegexp(
		t.Context(),
		`^core\.comment(char|string)$`,
		`^commit\.(cleanup|verbose)$`,
	) {
		require.NoError(t, err)
		switch entry.Key.Canonical() {
		case "core.commentchar":
			// Only use if commentString not already set.
			if ec.CommentString == "" {
				ec.CommentString = entry.Value
			}
		case "core.commentstring":
			// Takes precedence over commentChar.
			ec.CommentString = entry.Value
		case "commit.cleanup":
			ec.CleanupMode = entry.Value
		case "commit.verbose":
			// Git stores verbose as a boolean or int.
			// Any truthy value enables verbose mode.
			switch entry.Value {
			case "true", "1":
				ec.Verbose = true
			default:
				ec.Verbose = false
			}
		}
	}
	return ec
}

// openFixtureRepo builds a git repository
// from the given fixture script
// and returns an opened Repository handle.
func openFixtureRepo(
	t *testing.T,
	script string,
) *git.Repository {
	t.Helper()

	fixture, err := gittest.LoadFixtureScript(
		[]byte(script),
	)
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	repo, err := git.Open(
		t.Context(), fixture.Dir(),
		git.OpenOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)
	return repo
}

// setupMockedit configures the EDITOR environment
// to use mockedit, returning paths to the give and record files.
// The caller should write the desired editor output to givePath.
func setupMockedit(t *testing.T) (givePath, recordPath string) {
	t.Helper()

	dir := t.TempDir()
	givePath = filepath.Join(dir, "give")
	recordPath = filepath.Join(dir, "record")

	t.Setenv("GIT_EDITOR", "mockedit")
	t.Setenv("MOCKEDIT_GIVE", givePath)
	t.Setenv("MOCKEDIT_RECORD", recordPath)

	return givePath, recordPath
}

func TestIntegrationEditor_stripCleanup(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config commit.cleanup strip
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))

	givePath, _ := setupMockedit(t)
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("Real message\n# This is a comment\n\nBody text\n"),
		0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Real message")
	assert.Contains(t, msg, "Body text")
	assert.NotContains(t, msg, "# This is a comment")
}

func TestIntegrationEditor_scissorsCleanup(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config commit.cleanup scissors
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))

	givePath, _ := setupMockedit(t)
	content := "Keep this line\n" +
		"# " + _cutLine + "\n" +
		"Discard this line\n"
	require.NoError(t, os.WriteFile(
		givePath, []byte(content), 0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Keep this line")
	assert.NotContains(t, msg, "Discard this line")
}

func TestIntegrationEditor_whitespaceCleanup(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config commit.cleanup whitespace
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))

	givePath, _ := setupMockedit(t)
	// In whitespace mode, comments are preserved
	// but trailing whitespace and blank lines are cleaned.
	content := "Message line\n" +
		"# comment preserved\n" +
		"   \n" +
		"   \n"
	require.NoError(t, os.WriteFile(
		givePath, []byte(content), 0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Message line")
	assert.Contains(t, msg, "# comment preserved")
}

func TestIntegrationEditor_verbatimCleanup(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config commit.cleanup verbatim
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))

	givePath, _ := setupMockedit(t)
	// Verbatim mode preserves everything exactly.
	content := "  trailing spaces  \n# comment\n\n\n"
	require.NoError(t, os.WriteFile(
		givePath, []byte(content), 0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, content, buf.String())
}

func TestIntegrationEditor_commentChar(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config core.commentChar ';'
		git config commit.cleanup strip
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))
	require.Equal(t, ";", ec.CommentString)

	givePath, recordPath := setupMockedit(t)
	// The editor will receive instructions with ';' comments.
	// We write a message that includes ';'-prefixed lines
	// which should be stripped.
	content := "Real message\n; semicolon comment\n"
	require.NoError(t, os.WriteFile(
		givePath, []byte(content), 0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Real message")
	assert.NotContains(t, msg, "semicolon comment")

	// Verify the COMMIT_EDITMSG used ';' for comment lines.
	recorded, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "; ")
}

func TestIntegrationEditor_commentString(t *testing.T) {
	gittest.SkipUnlessVersionAtLeast(t,
		gittest.Version{Major: 2, Minor: 45, Patch: 0})

	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config core.commentString '//'
		git config commit.cleanup strip
		git commit --allow-empty -m 'Initial commit'
	`))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))
	require.Equal(t, "//", ec.CommentString)

	givePath, recordPath := setupMockedit(t)
	content := "Real message\n// slash comment\n"
	require.NoError(t, os.WriteFile(
		givePath, []byte(content), 0o644,
	))

	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
		Verbose:       ec.Verbose,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Real message")
	assert.NotContains(t, msg, "slash comment")

	// Verify the COMMIT_EDITMSG used '//' for comment lines.
	recorded, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "// ")
}

func TestIntegrationEditor_verbose(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		at '2024-01-01T00:01:00Z'
		cp file.txt file.txt
		git add file.txt
		git commit -m 'Add file'

		-- file.txt --
		hello world
	`))

	// Resolve commit and parent hashes.
	ctx := t.Context()
	commit, err := repo.PeelToCommit(ctx, "HEAD")
	require.NoError(t, err)
	parent, err := repo.PeelToCommit(ctx, "HEAD~1")
	require.NoError(t, err)

	givePath, recordPath := setupMockedit(t)
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("Verbose message\n"),
		0o644,
	))

	editor := &Editor{
		Repository: repo,
		Signals:    &nopSignalStack{},
		Log:        silogtest.New(t),
		Verbose:    true,
	}

	var buf bytes.Buffer
	err = editor.EditCommitMessage(
		ctx,
		strings.NewReader("original"),
		&buf,
		&EditCommitMessageOptions{
			Commit: commit,
			Parent: parent,
		},
	)
	require.NoError(t, err)
	msg := buf.String()
	assert.Contains(t, msg, "Verbose message")
	// The diff should not appear in the cleaned result.
	assert.NotContains(t, msg, "diff --git")

	// Verify COMMIT_EDITMSG contained the diff
	// and a scissors line.
	recorded, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "diff --git")
	assert.Contains(t, string(recorded), _cutLine)
}

func TestIntegrationEditor_prepareCommitMsgHook(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'
	`))

	// Install prepare-commit-msg hook
	// that appends a line via hook-helper.
	installHook(t, repo.GitDir(),
		"prepare-commit-msg")
	t.Setenv("HOOK_APPEND", "Hook-added-line")

	givePath, recordPath := setupMockedit(t)
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("Message from editor\n"),
		0o644,
	))

	editor := &Editor{
		Repository: repo,
		Signals:    &nopSignalStack{},
		Log:        silogtest.New(t),
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)

	// The hook appended a line before the editor ran,
	// and mockedit overwrote the file.
	// But the recorded COMMIT_EDITMSG should show
	// the hook's effect (hook runs before editor).
	recorded, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "Hook-added-line")

	// The final message comes from what mockedit wrote.
	assert.Contains(t, buf.String(), "Message from editor")
}

func TestIntegrationEditor_commitMsgHook(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git config commit.cleanup verbatim
		git commit --allow-empty -m 'Initial commit'
	`))

	// Install commit-msg hook that appends a trailer.
	installHook(t, repo.GitDir(), "commit-msg")
	t.Setenv("HOOK_APPEND", "Signed-off-by: Hook")

	givePath, _ := setupMockedit(t)
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("Message from editor\n"),
		0o644,
	))

	ec := loadEditorConfig(t, filepath.Dir(repo.GitDir()))
	editor := &Editor{
		Repository:    repo,
		Signals:       &nopSignalStack{},
		Log:           silogtest.New(t),
		CommentString: ec.CommentString,
		CleanupMode:   ec.CleanupMode,
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.NoError(t, err)

	// The commit-msg hook runs after the editor,
	// modifying the COMMIT_EDITMSG file
	// which is then read and returned.
	msg := buf.String()
	assert.Contains(t, msg, "Message from editor")
	assert.Contains(t, msg, "Signed-off-by: Hook")
}

func TestIntegrationEditor_hookFailure(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'
	`))

	// Install prepare-commit-msg hook that exits 1.
	installHook(t, repo.GitDir(),
		"prepare-commit-msg")
	t.Setenv("HOOK_EXIT_CODE", "1")

	givePath, _ := setupMockedit(t)
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("Message\n"),
		0o644,
	))

	editor := &Editor{
		Repository: repo,
		Signals:    &nopSignalStack{},
		Log:        silogtest.New(t),
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare-commit-msg")
}

func TestIntegrationEditor_emptyMessage(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	repo := openFixtureRepo(t, text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-01-01T00:00:00Z'

		git init
		git commit --allow-empty -m 'Initial commit'
	`))

	givePath, _ := setupMockedit(t)
	// Write empty/whitespace-only content.
	require.NoError(t, os.WriteFile(
		givePath,
		[]byte("   \n\n"),
		0o644,
	))

	editor := &Editor{
		Repository: repo,
		Signals:    &nopSignalStack{},
		Log:        silogtest.New(t),
	}

	var buf bytes.Buffer
	err := editor.EditCommitMessage(
		t.Context(),
		strings.NewReader("original"),
		&buf,
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty commit message")
}

// installHook creates a hook script that delegates
// to the hook-helper binary.
// The hook-helper behavior is controlled
// via HOOK_EXIT_CODE and HOOK_APPEND environment variables.
func installHook(t *testing.T, gitDir, hookName string) {
	t.Helper()

	hooksDir := filepath.Join(gitDir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))

	hookPath := filepath.Join(hooksDir, hookName)
	script := "#!/bin/sh\nexec hook-helper \"$@\"\n"
	require.NoError(t,
		os.WriteFile(hookPath, []byte(script), 0o755),
	)
}
