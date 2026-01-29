package claude

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrepareDiff_emptyDiff(t *testing.T) {
	_, err := PrepareDiff("", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no changes to process")
}

func TestPrepareDiff_allFilesFiltered(t *testing.T) {
	// Create a diff that will be completely filtered out.
	diff := `diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1,2 +1,3 @@
+new line
`
	// This test requires Claude CLI, so skip if not available.
	client := NewClient(nil)
	if !client.IsAvailable() {
		t.Skip("Claude CLI not available")
	}

	_, err := PrepareDiff(diff, nil)
	// The diff should be filtered out due to *.sum pattern.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no changes after filtering")
}

func TestPrepareDiff_validDiff(t *testing.T) {
	// Create a valid diff that should pass all checks.
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+// new comment
 func main() {}
`
	// This test requires Claude CLI, so skip if not available.
	client := NewClient(nil)
	if !client.IsAvailable() {
		t.Skip("Claude CLI not available")
	}

	result, err := PrepareDiff(diff, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.FilteredDiff)
	assert.NotNil(t, result.Config)
	assert.NotNil(t, result.Client)
}

func TestRunClaudeError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "NotAuthenticated",
			err:      ErrNotAuthenticated,
			contains: "not authenticated",
		},
		{
			name:     "RateLimited",
			err:      ErrRateLimited,
			contains: "rate limit",
		},
		{
			name:     "OtherError",
			err:      errors.New("some other error"),
			contains: "some other error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunClaudeError(tt.err)
			assert.Contains(t, err.Error(), tt.contains)
		})
	}
}
