package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_RebaseStopReason(t *testing.T) {
	tests := []struct {
		name       string
		done       string
		removeDone bool
		// withAmend simulates Git's "amend" marker file,
		// which Git writes only for deliberate stops where the commit
		// was applied cleanly and HEAD already holds it.
		withAmend bool
		want      git.RebaseInterruptKind
	}{
		{
			name: "Conflict",
			done: joinLines(
				"",
				" pick cc51432 Add bar",
				"",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			name: "Revert",
			done: joinLines(
				"pick cc51432 Add bar",
				"revert d62d116 Revert baz",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			name: "Fixup",
			done: joinLines(
				"pick cc51432 Add bar",
				"fixup d62d116 fixup! Add bar",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			name: "Squash",
			done: joinLines(
				"pick cc51432 Add bar",
				"squash d62d116 squash! Add bar",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			name: "Merge",
			done: joinLines(
				"pick cc51432 Add bar",
				"merge -C d62d116 refs/heads/side",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			// A deliberate "edit" stop applies the commit and
			// leaves an "amend" file behind.
			name: "Edit",
			done: joinLines(
				"pick cc51432 Add bar",
				"edit d62d116 Add baz",
			),
			withAmend: true,
			want:      git.RebaseInterruptDeliberate,
		},
		{
			name: "Break",
			done: joinLines(
				"pick cc51432 Add bar",
				"break",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			// An "edit" instruction whose commit conflicted while
			// being applied leaves "edit ..." as the last done line
			// but no "amend" file, since the commit never applied.
			// That must still be treated as a conflict.
			name: "EditConflict",
			done: joinLines(
				"pick cc51432 Add bar",
				"edit d62d116 Add baz",
			),
			withAmend: false,
			want:      git.RebaseInterruptConflict,
		},
		{
			name: "Reword",
			done: joinLines(
				"pick cc51432 Add bar",
				"reword d62d116 Add baz",
			),
			withAmend: true,
			want:      git.RebaseInterruptDeliberate,
		},
		{
			name: "RewordConflict",
			done: joinLines(
				"pick cc51432 Add bar",
				"reword d62d116 Add baz",
			),
			withAmend: false,
			want:      git.RebaseInterruptConflict,
		},
		{
			name: "Exec",
			done: joinLines(
				"pick cc51432 Add bar",
				"exec make test",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "Label",
			done: joinLines(
				"pick cc51432 Add bar",
				"label branch-point",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "Reset",
			done: joinLines(
				"pick cc51432 Add bar",
				"reset branch-point",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "UpdateRef",
			done: joinLines(
				"pick cc51432 Add bar",
				"update-ref refs/heads/feature",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "Noop",
			done: joinLines(
				"pick cc51432 Add bar",
				"noop",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "Drop",
			done: joinLines(
				"pick cc51432 Add bar",
				"drop d62d116 Add baz",
			),
			want: git.RebaseInterruptDeliberate,
		},
		{
			name: "UnknownCommand",
			done: joinLines(
				"pick cc51432 Add bar",
				"unknown d62d116 Add baz",
			),
			want: git.RebaseInterruptConflict,
		},
		{
			name:       "MissingDone",
			removeDone: true,
			want:       git.RebaseInterruptConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
				as 'Test <test@example.com>'
				at '2026-06-03T08:00:00-07:00'

				git init
				git commit --allow-empty -m 'Initial commit'

				git add bar.txt
				git commit -m 'Add bar'

				git checkout -b feature HEAD~
				mv conflicting-bar.txt bar.txt
				git add bar.txt
				git commit -m 'Conflicting bar'

				-- bar.txt --
				Contents of bar

				-- conflicting-bar.txt --
				Different contents of bar
			`)))
			require.NoError(t, err)
			t.Cleanup(fixture.Cleanup)

			wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
				Log: silogtest.New(t),
			})
			require.NoError(t, err)

			err = wt.Rebase(t.Context(), git.RebaseRequest{
				Branch:   "feature",
				Upstream: "main",
			})
			require.Error(t, err)
			require.ErrorAs(t, err, new(*git.RebaseInterruptError))

			stateDir := filepath.Join(fixture.Dir(), ".git", "rebase-merge")
			donePath := filepath.Join(stateDir, "done")
			if tt.removeDone {
				require.NoError(t, os.Remove(donePath))
			} else {
				require.NoError(t, os.WriteFile(donePath, []byte(tt.done), 0o644))
			}

			// Control the "amend" marker independently of the real
			// conflict that drove the rebase into this state.
			amendPath := filepath.Join(stateDir, "amend")
			if tt.withAmend {
				require.NoError(t, os.WriteFile(amendPath, []byte("amend"), 0o644))
			} else if err := os.Remove(amendPath); err != nil {
				require.ErrorIs(t, err, os.ErrNotExist)
			}

			got, err := wt.RebaseStopReason(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWorktree_RebaseStopReason_rewordAwaitingAmend(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2026-06-03T08:00:00-07:00'

		git init
		git config commit.gpgsign false
		git commit --allow-empty -m 'Initial commit'

		git add one.txt
		git commit -m 'Add one'

		-- one.txt --
		Contents of one
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	head, err := wt.Head(t.Context())
	require.NoError(t, err)

	// Use mockedit only for the rebase todo editor.
	// The commit-message editor must fail so Git stops after applying the
	// reworded commit and writes the amend marker.
	t.Setenv("GIT_SEQUENCE_EDITOR", "mockedit")
	t.Setenv("GIT_EDITOR", "false")
	mockedit.Expect(t).GiveLines("reword " + string(head) + " Add one")

	err = wt.Rebase(t.Context(), git.RebaseRequest{
		Upstream:    "HEAD~1",
		Interactive: true,
	})
	require.Error(t, err)
	defer func() {
		assert.NoError(t, wt.RebaseAbort(t.Context()))
	}()

	stateDir := filepath.Join(fixture.Dir(), ".git", "rebase-merge")
	done, err := os.ReadFile(filepath.Join(stateDir, "done"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(done), "reword "))

	_, err = os.Stat(filepath.Join(stateDir, "amend"))
	require.NoError(t, err)

	unmergedFiles, err := sliceutil.CollectErr(
		wt.ListFilesPaths(t.Context(), &git.ListFilesOptions{Unmerged: true}))
	require.NoError(t, err)
	assert.Empty(t, unmergedFiles)

	got, err := wt.RebaseStopReason(t.Context())
	require.NoError(t, err)
	assert.Equal(t, git.RebaseInterruptDeliberate, got)
}

func TestWorktree_RebaseStopReason_failedExec(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2026-06-03T08:00:00-07:00'

		git init
		git config commit.gpgsign false
		git commit --allow-empty -m 'Initial commit'

		git add one.txt
		git commit -m 'Add one'

		-- one.txt --
		Contents of one
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	// Use Git directly because Worktree.Rebase does not expose --exec.
	// This leaves the real sequencer state for a failed exec stop.
	cmd := exec.Command("git", "rebase", "-i", "--exec", "false", "HEAD~1")
	cmd.Dir = fixture.Dir()
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=true")
	err = cmd.Run()
	require.Error(t, err)
	defer func() {
		assert.NoError(t, wt.RebaseAbort(t.Context()))
	}()

	stateDir := filepath.Join(fixture.Dir(), ".git", "rebase-merge")
	done, err := os.ReadFile(filepath.Join(stateDir, "done"))
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(string(done)), "exec false"))

	_, err = os.Stat(filepath.Join(stateDir, "amend"))
	require.ErrorIs(t, err, os.ErrNotExist)

	unmergedFiles, err := sliceutil.CollectErr(
		wt.ListFilesPaths(t.Context(), &git.ListFilesOptions{Unmerged: true}))
	require.NoError(t, err)
	assert.Empty(t, unmergedFiles)

	got, err := wt.RebaseStopReason(t.Context())
	require.NoError(t, err)
	assert.Equal(t, git.RebaseInterruptDeliberate, got)
}
