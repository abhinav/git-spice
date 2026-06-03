package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_RebaseStopReason(t *testing.T) {
	tests := []struct {
		name       string
		done       string
		removeDone bool
		// withAmend simulates Git's "amend" marker file,
		// which it writes only for a deliberate "edit" stop
		// where the commit was applied cleanly.
		withAmend bool
		want      git.RebaseInterruptKind
	}{
		{
			name: "Conflict",
			done: "\n pick cc51432 Add bar\n\n",
			want: git.RebaseInterruptConflict,
		},
		{
			// A deliberate "edit" stop applies the commit and
			// leaves an "amend" file behind.
			name:      "Edit",
			done:      "pick cc51432 Add bar\nedit d62d116 Add baz\n",
			withAmend: true,
			want:      git.RebaseInterruptDeliberate,
		},
		{
			name: "Break",
			done: "pick cc51432 Add bar\nbreak\n",
			want: git.RebaseInterruptDeliberate,
		},
		{
			// An "edit" instruction whose commit conflicted while
			// being applied leaves "edit ..." as the last done line
			// but no "amend" file, since the commit never applied.
			// That must still be treated as a conflict.
			name:      "EditConflict",
			done:      "pick cc51432 Add bar\nedit d62d116 Add baz\n",
			withAmend: false,
			want:      git.RebaseInterruptConflict,
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
