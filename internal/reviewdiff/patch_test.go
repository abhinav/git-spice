package reviewdiff_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/reviewdiff"
)

func TestPatchContains(t *testing.T) {
	patch, err := reviewdiff.Parse(strings.NewReader(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@
 package main
 
+import "fmt"
 func main() {}
 
@@ -10,3 +11,4 @@ func helper() {
 	work()
+	fmt.Println("done")
 }
 
diff --git "a/with space.go" "b/with space.go"
--- "a/with space.go"
+++ "b/with space.go"
@@ -1 +1,2 @@
 package spaced
+var Added = true

diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go

diff --git a/script.sh b/script.sh
old mode 100644
new mode 100755
`))
	require.NoError(t, err)

	assert.True(t, patch.ContainsFile("main.go"))
	assert.True(t, patch.ContainsFile("with space.go"))
	assert.True(t, patch.ContainsFile("new.go"))
	assert.True(t, patch.ContainsFile("script.sh"))
	assert.False(t, patch.ContainsFile("old.go"))
	assert.False(t, patch.ContainsFile("missing.go"))

	assert.True(t, patch.ContainsLine("main.go", 3))
	assert.True(t, patch.ContainsLine("main.go", 13))
	assert.False(t, patch.ContainsLine("main.go", 8))
	assert.False(t, patch.ContainsLine("main.go", 0))
	assert.True(t, patch.ContainsLine("with space.go", 2))

	assert.True(t, patch.ContainsLineRange("main.go", 1, 5))
	assert.True(t, patch.ContainsLineRange("main.go", 11, 14))
	assert.False(t, patch.ContainsLineRange("main.go", 5, 11))
	assert.False(t, patch.ContainsLineRange("main.go", 4, 2))
}

func TestPatchDeletes(t *testing.T) {
	patch, err := reviewdiff.Parse(strings.NewReader(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -2,5 +2,4 @@ package main
 context
-delete one
-delete two
+replacement
 more context
 last context
diff --git a/old.go b/new.go
similarity index 80%
rename from old.go
rename to new.go
@@ -10,3 +10,2 @@
 keep
-removed
 keep too
`))
	require.NoError(t, err)

	assert.True(t, patch.DeletesLine("main.go", 3))
	assert.True(t, patch.DeletesLine("main.go", 4))
	assert.False(t, patch.DeletesLine("main.go", 2))
	assert.False(t, patch.DeletesLine("main.go", 5))
	assert.True(t, patch.DeletesLineRange("main.go", 1, 3))
	assert.False(t, patch.DeletesLineRange("main.go", 5, 7))
	assert.False(t, patch.DeletesLineRange("main.go", 0, 4))

	assert.True(t, patch.DeletesLine("old.go", 11))
	assert.False(t, patch.DeletesLine("new.go", 11))
}

func TestParseError(t *testing.T) {
	_, err := reviewdiff.Parse(strings.NewReader(`detached fragment
@@ -1 +1 @@
`))
	require.Error(t, err)
	assert.ErrorContains(t, err, "parse Git patch")
}
