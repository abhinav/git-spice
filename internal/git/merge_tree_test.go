package git_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

var gitMergeBaseVersion = gittest.Version{Major: 2, Minor: 45, Patch: 0}

func TestRepository_MergeTree(t *testing.T) {
	t.Parallel()

	gittest.SkipUnlessVersionAtLeast(t, gitMergeBaseVersion)

	t.Run("NoMergeBase", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T00:00:00Z'
			git init

			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b branch1 main
			git add file1.txt
			git commit -m 'Add file1'

			git checkout -b branch2 main
			git add file2.txt
			git commit -m 'Add file2'

			-- file.txt --
			initial content

			-- file1.txt --
			branch1 content

			-- file2.txt --
			branch2 content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		treeHash, err := repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1: "branch1",
			Branch2: "branch2",
		})
		require.NoError(t, err)

		// Verify the merged tree contains files from both branches.
		entries := make(map[string]git.Hash)
		for entry, err := range repo.ListTree(ctx, treeHash, git.ListTreeOptions{Recurse: true}) {
			require.NoError(t, err)
			entries[entry.Name] = entry.Hash
		}

		var buf bytes.Buffer
		if assert.Contains(t, entries, "file.txt") {
			require.NoError(t, repo.ReadObject(ctx, git.BlobType, entries["file.txt"], &buf))
			assert.Equal(t, "initial content\n\n", buf.String())
		}

		buf.Reset()
		if assert.Contains(t, entries, "file1.txt") {
			require.NoError(t, repo.ReadObject(ctx, git.BlobType, entries["file1.txt"], &buf))
			assert.Equal(t, "branch1 content\n\n", buf.String())
		}

		buf.Reset()
		if assert.Contains(t, entries, "file2.txt") {
			require.NoError(t, repo.ReadObject(ctx, git.BlobType, entries["file2.txt"], &buf))
			assert.Equal(t, "branch2 content\n", buf.String())
		}
	})

	t.Run("MergeBase", func(t *testing.T) {
		t.Parallel()
		gittest.SkipUnlessVersionAtLeast(t, gitMergeBaseVersion)

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T00:00:00Z'
			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b branch1
			git add file1.txt
			git commit -m 'Add file1'

			git checkout -b branch2
			git add file2.txt
			git commit -m 'Add file2'

			-- file.txt --
			initial content

			-- file1.txt --
			branch1 content

			-- file2.txt --
			branch2 content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		// Merge branch2 and main, using branch1 as the merge base.
		// This should exclude changes from branch1
		// in the resulting tree.
		treeHash, err := repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1:   "branch2",
			Branch2:   "main",
			MergeBase: "branch1",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, treeHash)

		entries := make(map[string]git.Hash)
		for entry, err := range repo.ListTree(ctx, treeHash, git.ListTreeOptions{Recurse: true}) {
			require.NoError(t, err)
			entries[entry.Name] = entry.Hash
		}

		assert.NotContains(t, entries, "file1.txt",
			"files from branch1 should not be included")

		var buf bytes.Buffer
		if assert.Contains(t, entries, "file.txt") {
			err = repo.ReadObject(ctx, git.BlobType, entries["file.txt"], &buf)
			require.NoError(t, err)
			assert.Equal(t, "initial content\n\n", buf.String())
		}

		buf.Reset()
		if assert.Contains(t, entries, "file2.txt") {
			err = repo.ReadObject(ctx, git.BlobType, entries["file2.txt"], &buf)
			require.NoError(t, err)
			assert.Equal(t, "branch2 content\n", buf.String())
		}
	})

	t.Run("Conflict", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T00:00:00Z'
			git init
			git add conflict.txt
			git commit -m 'Initial commit'

			git checkout -b branch1
			cp $WORK/branch1.txt conflict.txt
			git add conflict.txt
			git commit -m 'Branch1 change'

			git checkout -b branch2 main
			cp $WORK/branch2.txt conflict.txt
			git add conflict.txt
			git commit -m 'Branch2 change'

			-- conflict.txt --
			initial content

			-- branch1.txt --
			branch1 content

			-- branch2.txt --
			branch2 content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		_, err = repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1: "branch1",
			Branch2: "branch2",
		})
		require.Error(t, err)

		var conflictErr *git.MergeTreeConflictError
		require.ErrorAs(t, err, &conflictErr)

		assert.Equal(t, []string{"conflict.txt"}, slices.Collect(conflictErr.Filenames()))
		conflictTypes := make([]string, 0, len(conflictErr.Details))
		for _, d := range conflictErr.Details {
			conflictTypes = append(conflictTypes, d.Type)
		}
		assert.Contains(t, conflictTypes, "CONFLICT (contents)")
	})

	t.Run("ConflictFileInfo", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T00:00:00Z'
			git init
			git add main.txt
			git commit -m 'Initial commit'

			git checkout -b ours
			cp $WORK/ours.txt main.txt
			git add main.txt
			git commit -m 'Ours change'

			git checkout -b theirs main
			cp $WORK/theirs.txt main.txt
			git add main.txt
			git commit -m 'Theirs change'

			-- main.txt --
			line1
			line2
			line3

			-- ours.txt --
			line1
			ours-line2
			line3

			-- theirs.txt --
			line1
			theirs-line2
			line3
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		_, err = repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1: "ours",
			Branch2: "theirs",
		})
		require.Error(t, err)

		var conflictErr *git.MergeTreeConflictError
		require.ErrorAs(t, err, &conflictErr)

		// Find conflict files for each stage
		var baseFile, oursFile, theirsFile *git.MergeTreeConflictFile
		for i, f := range conflictErr.Files {
			if f.Path != "main.txt" {
				continue
			}

			assert.Equal(t, git.RegularMode, f.Mode)
			switch f.Stage {
			case git.ConflictStageBase:
				baseFile = &conflictErr.Files[i]
			case git.ConflictStageOurs:
				oursFile = &conflictErr.Files[i]
			case git.ConflictStageTheirs:
				theirsFile = &conflictErr.Files[i]
			}
		}
		require.NotNil(t, baseFile, "should have base version of conflicted file")
		require.NotNil(t, oursFile, "should have ours version of conflicted file")
		require.NotNil(t, theirsFile, "should have theirs version of conflicted file")

		// Read and verify contents of each version
		var baseBuf, oursBuf, theirsBuf bytes.Buffer
		require.NoError(t, repo.ReadObject(ctx, git.BlobType, baseFile.Object, &baseBuf))
		require.NoError(t, repo.ReadObject(ctx, git.BlobType, oursFile.Object, &oursBuf))
		require.NoError(t, repo.ReadObject(ctx, git.BlobType, theirsFile.Object, &theirsBuf))

		assert.Equal(t, "line1\nline2\nline3\n\n", baseBuf.String())
		assert.Equal(t, "line1\nours-line2\nline3\n\n", oursBuf.String())
		assert.Equal(t, "line1\ntheirs-line2\nline3\n", theirsBuf.String())
	})

	t.Run("MergeBaseTreeIsh", func(t *testing.T) {
		t.Parallel()
		gittest.SkipUnlessVersionAtLeast(t, gitMergeBaseVersion)

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T00:00:00Z'
			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b branch1
			git add file1.txt
			git commit -m 'Add file1'

			git checkout -b branch2 main
			git add file2.txt
			git commit -m 'Add file2'

			-- file.txt --
			initial content

			-- file1.txt --
			branch1 content

			-- file2.txt --
			branch2 content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)
		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		branch1Tree, err := repo.PeelToTree(ctx, "branch1")
		require.NoError(t, err)
		branch2Tree, err := repo.PeelToTree(ctx, "branch2")
		require.NoError(t, err)

		treeHash, err := repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1:   branch1Tree.String(),
			Branch2:   branch2Tree.String(),
			MergeBase: "main",
		})
		require.NoError(t, err)

		// Verify the merged tree contains files from both branches
		entries := make(map[string]struct{})
		for entry, err := range repo.ListTree(ctx, treeHash, git.ListTreeOptions{Recurse: true}) {
			require.NoError(t, err)
			entries[entry.Name] = struct{}{}
		}

		assert.Contains(t, entries, "file.txt")
		assert.Contains(t, entries, "file1.txt")
		assert.Contains(t, entries, "file2.txt")
	})

	t.Run("MergeBaseSameFile", func(t *testing.T) {
		t.Parallel()
		gittest.SkipUnlessVersionAtLeast(t, gitMergeBaseVersion)

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-06-21T12:22:23Z'
			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b branch1
			mv branch1.txt file.txt
			git add file.txt
			git commit -m 'Branch1 change'

			git checkout -b branch2
			mv branch2.txt file.txt
			git add file.txt
			git commit -m 'Branch2 change'

			-- file.txt --
			file
			spans
			multiple
			lines
			-- branch1.txt --
			file
			spans
			multiple
			lines
			and expands
			-- branch2.txt --
			this is a
			file that
			spans
			multiple
			lines
			and expands
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		// main -> branch1 -> branch2,
		// where all branches modify the same file.
		// We'll try to replay only branch2's changes on top of main.
		treeHash, err := repo.MergeTree(ctx, git.MergeTreeRequest{
			Branch1:   "branch2",
			Branch2:   "main",
			MergeBase: "branch1",
		})
		require.NoError(t, err)

		entries := make(map[string]git.Hash)
		for entry, err := range repo.ListTree(ctx, treeHash, git.ListTreeOptions{Recurse: true}) {
			require.NoError(t, err)
			entries[entry.Name] = entry.Hash
		}

		require.Contains(t, entries, "file.txt",
			"file.txt should be present in the merged tree")

		var buf bytes.Buffer
		err = repo.ReadObject(ctx, git.BlobType, entries["file.txt"], &buf)
		require.NoError(t, err)
		assert.Equal(t,
			"this is a\n"+
				"file that\n"+
				"spans\n"+
				"multiple\n"+
				"lines\n",
			buf.String(),
			"file.txt should contain main+branch2 changes only")
	})

	t.Run("AutomergeNoConflict", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-08-31T20:43:00Z'
			git init
			git add main.txt
			git commit -m 'Initial commit'

			git checkout -b above
			mv $WORK/above.txt main.txt
			git add main.txt
			git commit -m 'Add above'

			git checkout -b below main
			mv $WORK/below.txt main.txt
			git add main.txt
			git commit -m 'Add below'

			-- main.txt --
			foo
			bar
			-- below.txt --
			foo
			baz
			-- above.txt --
			baz
			foo
			bar
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		treeHash, err := repo.MergeTree(ctx, git.MergeTreeRequest{
			MergeBase: "main",
			Branch1:   "below",
			Branch2:   "above",
		})
		require.NoError(t, err)

		blobHash, err := repo.HashAt(ctx, treeHash.String(), "main.txt")
		require.NoError(t, err)

		var buf bytes.Buffer
		err = repo.ReadObject(ctx, git.BlobType, blobHash, &buf)
		require.NoError(t, err)

		assert.Equal(t, "baz\nfoo\nbaz\n", buf.String())
	})

	t.Run("AutomergeWithConflict", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			at '2025-08-31T20:43:00Z'
			git init
			git add main.txt
			git commit -m 'Initial commit'

			git checkout -b above
			mv $WORK/above.txt main.txt
			git add main.txt
			git commit -m 'Add above'

			git checkout -b below main
			mv $WORK/below.txt main.txt
			git add main.txt
			git commit -m 'Add below'

			-- main.txt --
			foo
			bar
			baz
			-- below.txt --
			foo
			bar
			-- above.txt --
			foo
			baz
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		req := git.MergeTreeRequest{
			MergeBase: "main",
			Branch1:   "below",
			Branch2:   "above",
		}
		git.SetConflictStyle(&req, "merge")
		treeHash, err := repo.MergeTree(ctx, req)
		require.Error(t, err)

		var conflictErr *git.MergeTreeConflictError
		require.ErrorAs(t, err, &conflictErr)
		assert.Equal(t, []string{"main.txt"}, slices.Collect(conflictErr.Filenames()))

		// There should be both an automatic resolution
		// and a blocking conflict.
		typesToFiles := make(map[string]map[string]struct{}) // type -> set of files
		for _, d := range conflictErr.Details {
			for _, p := range d.Paths {
				if typesToFiles[d.Type] == nil {
					typesToFiles[d.Type] = make(map[string]struct{})
				}
				typesToFiles[d.Type][p] = struct{}{}
			}
		}

		assert.Equal(t, map[string]map[string]struct{}{
			"Auto-merging":        {"main.txt": {}},
			"CONFLICT (contents)": {"main.txt": {}},
		}, typesToFiles)

		blobHash, err := repo.HashAt(ctx, treeHash.String(), "main.txt")
		require.NoError(t, err)

		var buf bytes.Buffer
		err = repo.ReadObject(ctx, git.BlobType, blobHash, &buf)
		require.NoError(t, err)

		assert.Equal(t, joinLines(
			"foo",
			"<<<<<<< below",
			"bar",
			"=======",
			"baz",
			">>>>>>> above",
		), buf.String())
	})
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
