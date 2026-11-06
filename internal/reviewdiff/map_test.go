package reviewdiff_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/reviewdiff"
)

func TestPatchMapAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		patch  string
		anchor review.Anchor
		want   review.Anchor
		wantOK bool
	}{
		{
			name: "EarlierInsertionShiftsRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
+zero
 one
 two
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
			want:   review.Anchor{Path: "main.go", StartLine: 3, EndLine: 3},
			wantOK: true,
		},
		{
			name: "InternalInsertionExpandsRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 one
+inserted
 two
 three
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 1, EndLine: 2},
			want:   review.Anchor{Path: "main.go", StartLine: 1, EndLine: 3},
			wantOK: true,
		},
		{
			name: "InsertionAtStartShiftsRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,0 +2 @@
+inserted
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 2, EndLine: 3},
			want:   review.Anchor{Path: "main.go", StartLine: 3, EndLine: 4},
			wantOK: true,
		},
		{
			name: "InsertionAfterEndKeepsRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -3,0 +4 @@
+inserted
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 2, EndLine: 3},
			want:   review.Anchor{Path: "main.go", StartLine: 2, EndLine: 3},
			wantOK: true,
		},
		{
			name: "InternalDeletionShrinksRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,4 +1,3 @@
 one
-two
 three
 four
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 1, EndLine: 3},
			want:   review.Anchor{Path: "main.go", StartLine: 1, EndLine: 2},
			wantOK: true,
		},
		{
			name: "ReplacementMapsToReplacementRange",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 one
-two
+replacement one
+replacement two
 three
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
			want:   review.Anchor{Path: "main.go", StartLine: 2, EndLine: 3},
			wantOK: true,
		},
		{
			name: "RenameMapsPath",
			patch: `diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
`,
			anchor: review.Anchor{Path: "old.go", StartLine: 4, EndLine: 6},
			want:   review.Anchor{Path: "new.go", StartLine: 4, EndLine: 6},
			wantOK: true,
		},
		{
			name: "RenameMapsFile",
			patch: `diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
`,
			anchor: review.Anchor{Path: "old.go"},
			want:   review.Anchor{Path: "new.go"},
			wantOK: true,
		},
		{
			name: "CopyKeepsOriginalPath",
			patch: `diff --git a/original.go b/copy.go
similarity index 100%
copy from original.go
copy to copy.go
`,
			anchor: review.Anchor{Path: "original.go", StartLine: 4, EndLine: 6},
			want:   review.Anchor{Path: "original.go", StartLine: 4, EndLine: 6},
			wantOK: true,
		},
		{
			name: "DeletedRangeDisappears",
			patch: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,2 @@
 one
-two
 three
`,
			anchor: review.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
			wantOK: false,
		},
		{
			name: "DeletedFileDisappears",
			patch: `diff --git a/main.go b/main.go
deleted file mode 100644
--- a/main.go
+++ /dev/null
@@ -1 +0,0 @@
-package main
`,
			anchor: review.Anchor{Path: "main.go"},
			wantOK: false,
		},
		{
			name:   "UnchangedFileKeepsAnchor",
			patch:  "",
			anchor: review.Anchor{Path: "main.go", StartLine: 4, EndLine: 6},
			want:   review.Anchor{Path: "main.go", StartLine: 4, EndLine: 6},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			patch, err := reviewdiff.Parse(strings.NewReader(tt.patch))
			require.NoError(t, err)

			got, ok := patch.MapAnchor(tt.anchor)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
