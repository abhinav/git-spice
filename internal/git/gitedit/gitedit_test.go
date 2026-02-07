package gitedit

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

//go:generate mockgen -destination=mock_repository_test.go -package=gitedit -write_package_comment=false -typed=true . Repository

// nopSignalStack is a no-op SignalStack for tests.
type nopSignalStack struct{}

func (*nopSignalStack) Notify(chan<- os.Signal, ...os.Signal) {}
func (*nopSignalStack) Stop(chan<- os.Signal)                 {}

func TestEditor_EditCommitMessage(t *testing.T) {
	t.Run("HappyPath", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, "modified message\n"), nil)

		// Stripspace for comment instructions.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		// No hooks.
		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		// Cleanup: strip mode.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{StripComments: true},
			).
			DoAndReturn(passthroughStripspace)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			nil,
		)
		require.NoError(t, err)
		assert.Equal(t, "modified message\n", buf.String())
	})

	t.Run("NoVerifySkipsOnlyCommitMsg", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, "modified message\n"), nil)
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))
		repo.EXPECT().
			HookRun(
				gomock.Any(),
				"prepare-commit-msg",
				gomock.Any(),
			).
			Return(nil)
		// No commit-msg expectation:
		// --no-verify must skip only this hook.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{StripComments: true},
			).
			DoAndReturn(passthroughStripspace)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			&EditCommitMessageOptions{NoVerify: true},
		)
		require.NoError(t, err)
		assert.Equal(t, "modified message\n", buf.String())
	})

	t.Run("EnvironmentPassedToHooksAndEditor", func(t *testing.T) {
		t.Setenv("GITEDIT_INHERITED_ENV", "present")
		contentFile := filepath.Join(t.TempDir(), "content")
		require.NoError(
			t,
			os.WriteFile(contentFile, []byte("modified message\n"), 0o644),
		)
		t.Setenv("EDITOR_ENV_GIVE", contentFile)
		t.Setenv("EDITOR_ENV_INHERITED_NAME", "GITEDIT_INHERITED_ENV")
		t.Setenv("EDITOR_ENV_INHERITED_WANT", "present")
		t.Setenv("EDITOR_ENV_ADDED_NAME", "GIT_INDEX_FILE")
		t.Setenv("EDITOR_ENV_ADDED_WANT", "/tmp/git-spice-test-index")

		repo := &recordingRepository{
			editor: "editor-env-helper",
		}
		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			&EditCommitMessageOptions{
				Env: []string{
					"GIT_INDEX_FILE=/tmp/git-spice-test-index",
				},
			},
		)
		require.NoError(t, err)
		assert.Equal(t, "modified message\n", buf.String())
		assert.Equal(t, [][]string{
			{"GIT_INDEX_FILE=/tmp/git-spice-test-index"},
			{"GIT_INDEX_FILE=/tmp/git-spice-test-index"},
		}, repo.hookEnv)
	})

	t.Run("EmptyMessage", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, ""), nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		// Cleanup returns empty.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{StripComments: true},
			).
			DoAndReturn(passthroughStripspace)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			nil,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty commit message")
	})

	t.Run("EditorFails", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(failingEditorScript(t), nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			nil,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "editor")
	})

	t.Run("PrepareCommitMsgHookFails", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return("true", nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(assert.AnError)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			nil,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prepare-commit-msg")
	})

	t.Run("CommitMsgHookFails", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, "new message\n"), nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(assert.AnError)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original message"),
			&buf,
			nil,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "commit-msg")
	})

	t.Run("Verbose", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		// The editor script will verify
		// that the diff is present in the file.
		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(
				verifyingEditorScript(
					t,
					"diff --git",
					"edited message\n",
				),
				nil,
			)

		// Comment lines for instructions.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#")).
			Times(2) // instructions + scissors instructions

		repo.EXPECT().
			DiffTreePatch(
				gomock.Any(),
				gomock.Any(),
				"aaa", "bbb",
			).
			DoAndReturn(func(
				_ context.Context,
				w io.Writer,
				_, _ string,
			) error {
				_, err := io.WriteString(
					w,
					"diff --git a/foo b/foo\n"+
						"--- a/foo\n"+
						"+++ b/foo\n"+
						"@@ -1 +1 @@\n"+
						"-old\n"+
						"+new\n",
				)
				return err
			})

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{StripComments: true},
			).
			DoAndReturn(passthroughStripspace)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "strip",
			Verbose:       true,
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original"),
			&buf,
			&EditCommitMessageOptions{
				Commit: "bbb",
				Parent: "aaa",
			},
		)
		require.NoError(t, err)
		assert.Equal(t, "edited message\n", buf.String())
	})

	t.Run("ScissorsCleanup", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		// Write a message that includes a scissors line.
		editorContent := "keep this\n" +
			"# " + _cutLine + "\n" +
			"discard this\n"
		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, editorContent), nil)

		// Scissors mode: no instructions written.

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		// Cleanup: scissors mode uses plain stripspace.
		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				(*git.StripspaceOptions)(nil),
			).
			DoAndReturn(passthroughStripspace)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "scissors",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original"),
			&buf,
			nil,
		)
		require.NoError(t, err)
		assert.Equal(t, "keep this\n", buf.String())
	})

	t.Run("VerbatimCleanup", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		content := "  message with whitespace  \n\n"
		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, content), nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		editor := &Editor{
			Repository:    repo,
			Signals:       &nopSignalStack{},
			Log:           silog.Nop(),
			CommentString: "#",
			CleanupMode:   "verbatim",
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original"),
			&buf,
			nil,
		)
		require.NoError(t, err)
		// Verbatim returns the content as-is.
		assert.Equal(t, content, buf.String())
	})

	t.Run("DefaultCommentString", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		repo := NewMockRepository(mockCtrl)

		repo.EXPECT().
			Var(gomock.Any(), "GIT_EDITOR").
			Return(editorScript(t, "msg\n"), nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{CommentLines: true},
			).
			DoAndReturn(commentLinesStripspace("#"))

		repo.EXPECT().
			HookRun(gomock.Any(), "prepare-commit-msg", gomock.Any()).
			Return(nil)
		repo.EXPECT().
			HookRun(gomock.Any(), "commit-msg", gomock.Any()).
			Return(nil)

		repo.EXPECT().
			Stripspace(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				&git.StripspaceOptions{StripComments: true},
			).
			DoAndReturn(passthroughStripspace)

		// Empty CommentString and CleanupMode
		// to test defaults.
		editor := &Editor{
			Repository: repo,
			Signals:    &nopSignalStack{},
			Log:        silog.Nop(),
		}

		var buf bytes.Buffer
		err := editor.EditCommitMessage(
			t.Context(),
			strings.NewReader("original"),
			&buf,
			nil,
		)
		require.NoError(t, err)
		assert.Equal(t, "msg\n", buf.String())
	})
}

func TestScissorsReader(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		raw     string
		want    string
	}{
		{
			name:    "NoScissors",
			comment: "#",
			raw:     "hello\nworld\n",
			want:    "hello\nworld\n",
		},
		{
			name:    "WithScissors",
			comment: "#",
			raw: "keep\n" +
				"# " + _cutLine + "\n" +
				"discard\n",
			want: "keep\n",
		},
		{
			name:    "TrailingWhitespace",
			comment: "#",
			raw: "keep\n" +
				"# " + _cutLine + "  \n" +
				"discard\n",
			want: "keep\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newScissorsReader(
				strings.NewReader(tt.raw),
				tt.comment,
			)
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

// editorScript returns an editor command
// that writes the given content to the file argument.
func editorScript(t *testing.T, content string) string {
	t.Helper()

	src := filepath.Join(t.TempDir(), "content")
	require.NoError(t, os.WriteFile(src, []byte(content), 0o644))
	t.Setenv("EDITOR_COPY_GIVE", src)
	return "editor-copy"
}

// failingEditorScript returns an editor command
// that exits with a non-zero status.
func failingEditorScript(t *testing.T) string {
	t.Helper()
	return "editor-fail"
}

// verifyingEditorScript returns an editor command
// that verifies the file contains the expected substring,
// then writes the given content.
func verifyingEditorScript(
	t *testing.T,
	wantSubstr string,
	newContent string,
) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "content")
	require.NoError(
		t,
		os.WriteFile(src, []byte(newContent), 0o644),
	)
	t.Setenv("EDITOR_COPY_GIVE", src)
	t.Setenv("EDITOR_VERIFY_WANT", wantSubstr)
	return "editor-verify"
}

// commentLinesStripspace returns a Stripspace DoAndReturn
// that prepends the given comment string to each line.
func commentLinesStripspace(
	comment string,
) func(context.Context, io.Reader, io.Writer, *git.StripspaceOptions) error {
	return func(
		_ context.Context,
		r io.Reader,
		w io.Writer,
		_ *git.StripspaceOptions,
	) error {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			return err
		}
		for line := range strings.SplitSeq(
			strings.TrimSuffix(buf.String(), "\n"),
			"\n",
		) {
			if _, err := io.WriteString(
				w, comment+" "+line+"\n",
			); err != nil {
				return err
			}
		}
		return nil
	}
}

// passthroughStripspace is a Stripspace DoAndReturn
// that copies input to output unchanged.
func passthroughStripspace(
	_ context.Context,
	r io.Reader,
	w io.Writer,
	_ *git.StripspaceOptions,
) error {
	_, err := io.Copy(w, r)
	return err
}

type recordingRepository struct {
	editor  string
	hookEnv [][]string
}

func (r *recordingRepository) Var(
	context.Context,
	string,
) (string, error) {
	return r.editor, nil
}

func (r *recordingRepository) DiffTreePatch(
	context.Context,
	io.Writer,
	string,
	string,
) error {
	return nil
}

func (r *recordingRepository) Stripspace(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	opts *git.StripspaceOptions,
) error {
	if opts != nil && opts.CommentLines {
		return commentLinesStripspace("#")(ctx, in, out, opts)
	}
	return passthroughStripspace(ctx, in, out, opts)
}

func (r *recordingRepository) HookRun(
	_ context.Context,
	_ string,
	opts *git.HookRunOptions,
) error {
	if opts == nil {
		r.hookEnv = append(r.hookEnv, nil)
		return nil
	}
	r.hookEnv = append(r.hookEnv, opts.Env)
	return nil
}
