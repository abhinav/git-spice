package submodule

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrSubmoduleNotInitialized indicates that a submodule has not been
// initialized with `gs repo init`. It is a soft signal: callers that
// iterate submodules treat it as "skip" rather than a hard error.
var ErrSubmoduleNotInitialized = errors.New("submodule not initialized with git-spice")

// DivergedFromRecordError reports that a submodule is on a branch
// other than the one recorded for the current parent branch,
// while the caller was about to record state that depends on the
// recorded association (e.g., committing with staged sub content).
//
// The error message includes both copy-pasteable remediation strings:
//
//	git -C <path> checkout <recorded>
//
// or
//
//	gs branch submodule repoint <path> -b <current>
type DivergedFromRecordError struct {
	Path           string
	RecordedBranch string
	CurrentBranch  string
}

// Error implements the error interface.
func (e *DivergedFromRecordError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"submodule %s: on branch %s but parent records %s",
		e.Path, e.CurrentBranch, e.RecordedBranch,
	)
	b.WriteString("\n\nResolve by either:\n  git -C ")
	b.WriteString(e.Path)
	b.WriteString(" checkout ")
	b.WriteString(e.RecordedBranch)
	b.WriteString("\nor:\n  gs branch submodule repoint ")
	b.WriteString(e.Path)
	b.WriteString(" -b ")
	b.WriteString(e.CurrentBranch)
	return b.String()
}

// FoldConflict identifies a single conflicting submodule association
// when folding two branches that record different sub branches.
type FoldConflict struct {
	Path        string
	BaseBranch  string
	ChildBranch string
}

// FoldConflictError reports unresolved submodule association conflicts
// during a `gs branch fold` operation in non-interactive mode.
//
// Conflicts are unresolved when the base and folded branches record
// different sub-branches for the same path and the user did not pass
// `--module-branch=<path>=<branch>` to choose.
type FoldConflictError struct {
	Conflicts []FoldConflict
}

// Error implements the error interface.
func (e *FoldConflictError) Error() string {
	if len(e.Conflicts) == 0 {
		return "submodule fold conflict"
	}

	sorted := make([]FoldConflict, len(e.Conflicts))
	copy(sorted, e.Conflicts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	var b strings.Builder
	b.WriteString("submodule fold conflicts:\n")
	for _, c := range sorted {
		fmt.Fprintf(&b,
			"  %s: base records %s, folded branch records %s\n",
			c.Path, c.BaseBranch, c.ChildBranch,
		)
	}
	b.WriteString("\nResolve by either:\n")
	for _, c := range sorted {
		fmt.Fprintf(&b,
			"  --module-branch=%s=<%s|%s>\n",
			c.Path, c.BaseBranch, c.ChildBranch,
		)
	}
	b.WriteString(
		"or run 'gs branch submodule repoint' after the fold completes.",
	)
	return b.String()
}
