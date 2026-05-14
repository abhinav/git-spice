package submodule_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/handler/submodule"
)

func TestDivergedFromRecordError_format(t *testing.T) {
	t.Parallel()

	err := &submodule.DivergedFromRecordError{
		Path:           "libs/core",
		RecordedBranch: "feat-x",
		CurrentBranch:  "feat-y",
	}
	msg := err.Error()

	// Includes both copy-pasteable remediation commands.
	assert.Contains(t, msg, "libs/core")
	assert.Contains(t, msg, "feat-x")
	assert.Contains(t, msg, "feat-y")
	assert.Contains(t, msg, "git -C libs/core checkout feat-x")
	assert.Contains(t, msg, "gs branch submodule repoint libs/core -b feat-y")
}

func TestFoldConflictError_format(t *testing.T) {
	t.Parallel()

	err := &submodule.FoldConflictError{
		Conflicts: []submodule.FoldConflict{
			{Path: "libs/b", BaseBranch: "main", ChildBranch: "feat-b"},
			{Path: "libs/a", BaseBranch: "main", ChildBranch: "feat-a"},
		},
	}
	msg := err.Error()

	// Sorted by path.
	idxA := strings.Index(msg, "libs/a")
	idxB := strings.Index(msg, "libs/b")
	assert.True(t, idxA >= 0 && idxB > idxA, "paths must be sorted")

	// Each path appears with both branches and a --module-branch hint.
	assert.Contains(t, msg, "--module-branch=libs/a=<main|feat-a>")
	assert.Contains(t, msg, "--module-branch=libs/b=<main|feat-b>")
}

func TestFoldConflictError_empty(t *testing.T) {
	t.Parallel()

	err := &submodule.FoldConflictError{}
	assert.Equal(t, "submodule fold conflict", err.Error())
}
