package diffmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapper_Map(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		file     string
		line     int
		wantPath string
		wantLine int
		wantSide string
		wantErr  string
	}{
		{
			name: "AddedLine",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+func hello() {}
 func main() {}
`,
			file:     "main.go",
			line:     3,
			wantPath: "main.go",
			wantLine: 3,
			wantSide: "RIGHT",
		},
		{
			name: "ContextLine",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+func hello() {}
 func main() {}
`,
			file:     "main.go",
			line:     1,
			wantPath: "main.go",
			wantLine: 1,
			wantSide: "RIGHT",
		},
		{
			name: "MultipleHunks",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+import "fmt"
 func main() {}
@@ -10,3 +11,4 @@
 func foo() {}

+func bar() {}
 func baz() {}
`,
			file:     "main.go",
			line:     13,
			wantPath: "main.go",
			wantLine: 13,
			wantSide: "RIGHT",
		},
		{
			name: "FileNotInDiff",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+func hello() {}
 func main() {}
`,
			file:    "other.go",
			line:    1,
			wantErr: `file "other.go" not in diff`,
		},
		{
			name: "LineNotInDiff",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+func hello() {}
 func main() {}
`,
			file:    "main.go",
			line:    100,
			wantErr: `line 100 of "main.go" not in diff`,
		},
		{
			name: "MultipleFiles",
			diff: `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package a
+func A() {}

diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,2 +1,3 @@
 package b
+func B() {}

`,
			file:     "b.go",
			line:     2,
			wantPath: "b.go",
			wantLine: 2,
			wantSide: "RIGHT",
		},
		{
			name: "NewFile",
			diff: `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package new
+
+func New() {}
`,
			file:     "new.go",
			line:     1,
			wantPath: "new.go",
			wantLine: 1,
			wantSide: "RIGHT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New([]byte(tt.diff))
			require.NoError(t, err)

			path, line, side, err := m.Map(tt.file, tt.line)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, path)
			assert.Equal(t, tt.wantLine, line)
			assert.Equal(t, tt.wantSide, side)
		})
	}
}

func TestMapper_Files(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package a
+func A() {}

diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,2 +1,3 @@
 package b
+func B() {}

`
	m, err := New([]byte(diff))
	require.NoError(t, err)

	files := m.Files()
	assert.Len(t, files, 2)
	assert.Contains(t, files, "a.go")
	assert.Contains(t, files, "b.go")
}

func TestNew_EmptyDiff(t *testing.T) {
	m, err := New([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, m.Files())
}
