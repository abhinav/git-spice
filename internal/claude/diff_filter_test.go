package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDiff(t *testing.T) {
	t.Run("SingleFile", func(t *testing.T) {
		diff := `diff --git a/foo.go b/foo.go
index 1234567..abcdefg 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo

+// New comment
 func Foo() {}
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "foo.go", files[0].Path)
		assert.False(t, files[0].Binary)
		assert.Contains(t, files[0].Content, "+// New comment")
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		diff := `diff --git a/foo.go b/foo.go
index 1234567..abcdefg 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo
+// comment
diff --git a/bar.go b/bar.go
index 1234567..abcdefg 100644
--- a/bar.go
+++ b/bar.go
@@ -1,3 +1,4 @@
 package bar
+// comment
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 2)
		assert.Equal(t, "foo.go", files[0].Path)
		assert.Equal(t, "bar.go", files[1].Path)
	})

	t.Run("QuotedFilenames", func(t *testing.T) {
		diff := `diff --git "a/path with spaces.go" "b/path with spaces.go"
index 1234567..abcdefg 100644
--- "a/path with spaces.go"
+++ "b/path with spaces.go"
@@ -1,3 +1,4 @@
 package foo
+// comment
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "path with spaces.go", files[0].Path)
	})

	t.Run("BinaryFile", func(t *testing.T) {
		diff := `diff --git a/image.png b/image.png
Binary files a/image.png and b/image.png differ
diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo
+// comment
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 2)
		assert.Equal(t, "image.png", files[0].Path)
		assert.True(t, files[0].Binary)
		assert.Equal(t, "foo.go", files[1].Path)
		assert.False(t, files[1].Binary)
	})

	t.Run("NewFile", func(t *testing.T) {
		diff := `diff --git a/new.go b/new.go
new file mode 100644
index 0000000..abcdefg
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package new
+
+func New() {}
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "new.go", files[0].Path)
	})

	t.Run("DeletedFile", func(t *testing.T) {
		diff := `diff --git a/old.go b/old.go
deleted file mode 100644
index abcdefg..0000000
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package old
-
-func Old() {}
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "old.go", files[0].Path)
	})

	t.Run("RenamedFile", func(t *testing.T) {
		diff := `diff --git a/old.go b/new.go
similarity index 90%
rename from old.go
rename to new.go
index 1234567..abcdefg 100644
--- a/old.go
+++ b/new.go
@@ -1,3 +1,4 @@
 package foo
+// comment
`
		files, err := ParseDiff(diff)
		require.NoError(t, err)
		require.Len(t, files, 1)
		// For renames, use the destination path.
		assert.Equal(t, "new.go", files[0].Path)
	})
}

func TestFilterDiff(t *testing.T) {
	t.Run("ExcludePatterns", func(t *testing.T) {
		files := []DiffFile{
			{Path: "go.sum", Content: "some content"},
			{Path: "foo.go", Content: "package foo"},
			{Path: "vendor/lib/lib.go", Content: "package lib"},
			{Path: "generated.pb.go", Content: "package gen"},
		}

		patterns := []string{"*.sum", "vendor/*", "*.pb.go"}
		result := FilterDiff(files, patterns)

		require.Len(t, result, 1)
		assert.Equal(t, "foo.go", result[0].Path)
	})

	t.Run("ExcludeBinaryFiles", func(t *testing.T) {
		files := []DiffFile{
			{Path: "image.png", Binary: true, Content: ""},
			{Path: "foo.go", Binary: false, Content: "package foo"},
		}

		result := FilterDiff(files, nil)

		require.Len(t, result, 1)
		assert.Equal(t, "foo.go", result[0].Path)
	})

	t.Run("NoPatterns", func(t *testing.T) {
		files := []DiffFile{
			{Path: "foo.go", Content: "package foo"},
			{Path: "bar.go", Content: "package bar"},
		}

		result := FilterDiff(files, nil)
		require.Len(t, result, 2)
	})
}

func TestCheckBudget(t *testing.T) {
	t.Run("UnderBudget", func(t *testing.T) {
		files := []DiffFile{
			{Path: "foo.go", Content: "line1\nline2\nline3"},
		}

		result := CheckBudget(files, 100)
		assert.False(t, result.OverBudget)
		assert.Equal(t, 3, result.TotalLines)
	})

	t.Run("OverBudget", func(t *testing.T) {
		files := []DiffFile{
			{Path: "foo.go", Content: "line1\nline2\nline3\nline4\nline5"},
			{Path: "bar.go", Content: "line1\nline2\nline3"},
		}

		result := CheckBudget(files, 5)
		assert.True(t, result.OverBudget)
		assert.Equal(t, 8, result.TotalLines)
		assert.Equal(t, 5, result.MaxLines)
	})

	t.Run("FileLineCounts", func(t *testing.T) {
		files := []DiffFile{
			{Path: "big.go", Content: "1\n2\n3\n4\n5\n6\n7\n8\n9\n10"},
			{Path: "small.go", Content: "1\n2"},
		}

		result := CheckBudget(files, 5)
		require.Len(t, result.FileLines, 2)
		assert.Equal(t, 10, result.FileLines["big.go"])
		assert.Equal(t, 2, result.FileLines["small.go"])
	})
}

func TestReconstructDiff(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		files := []DiffFile{
			{Path: "foo.go", Content: "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n"},
			{Path: "bar.go", Content: "diff --git a/bar.go b/bar.go\n--- a/bar.go\n+++ b/bar.go\n"},
		}

		result := ReconstructDiff(files)
		assert.Contains(t, result, "diff --git a/foo.go")
		assert.Contains(t, result, "diff --git a/bar.go")
	})
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{"Empty", "", 0},
		{"OneLine", "hello", 1},
		{"TwoLines", "hello\nworld", 2},
		{"TrailingNewline", "hello\nworld\n", 2},
		{"MultipleLines", "a\nb\nc\nd\ne", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, countLines(tt.content))
		})
	}
}
