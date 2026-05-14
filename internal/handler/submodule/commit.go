package submodule

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// CommitMode selects which submodule-side commit operation to run
// for staged content in a tracked sub.
type CommitMode int

const (
	// CommitModeCreate makes a new commit in the sub (matches `gs cc`).
	CommitModeCreate CommitMode = iota
	// CommitModeAmend amends the sub's HEAD commit (matches non-interactive `gs ca`).
	CommitModeAmend
)

// CommitMessageSource bundles the inputs that determine the commit
// message and behavior for both parent and submodule commits.
type CommitMessageSource struct {
	Message       string
	MessageFile   string
	NoEdit        bool // amend only
	ModuleMessage map[string]string
	Signoff       bool
	NoVerify      bool
	All           bool
}

// PreCommitSubmodules performs submodule-side commit operations and
// stages gitlink updates in the parent so a subsequent parent commit
// reflects sub state changes in a single commit.
//
// For each tracked submodule on parentBranch, it applies the three-
// state model documented in the plan:
//
//	State 1: sub has staged content + is on the recorded branch
//	    → auto-commit (or amend) in sub; stage new gitlink in parent.
//	State 2: sub has no staged content
//	    → if sub HEAD moved past the gitlink in parent's HEAD,
//	      stage the new gitlink. Otherwise no-op.
//	State 3: sub has staged content but is on a different branch
//	    → return DivergedFromRecordError before any work.
//
// Returns the list of sub paths whose gitlink was staged.
func (a *Applier) PreCommitSubmodules(
	ctx context.Context,
	parentBranch string,
	mode CommitMode,
	msg CommitMessageSource,
) (staged []string, err error) {
	resp, err := a.Store.LookupBranch(ctx, parentBranch)
	if err != nil {
		// Not tracked yet — nothing to do.
		if isErrNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"lookup branch %s: %w", parentBranch, err,
		)
	}
	if len(resp.Submodules) == 0 {
		return nil, nil
	}

	paths := make([]string, 0, len(resp.Submodules))
	for p := range resp.Submodules {
		if a.isExcluded(p) {
			continue
		}
		paths = append(paths, p)
	}
	slices.Sort(paths)

	// First pass: state checks; bail before mutating anything on
	// state-3 errors so the parent commit doesn't run with a
	// half-applied submodule state.
	type plan struct {
		path         string
		recorded     string
		status       *git.SubmoduleStatus
		hasStaged    bool
		shouldCommit bool
	}
	var todo []plan
	for _, p := range paths {
		recorded := resp.Submodules[p]
		subWt, err := a.Worktree.SubmoduleWorktree(ctx, p)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: open worktree: %w", p, err,
			)
		}

		// -a flag: stage tracked-and-modified files inside the sub.
		if msg.All {
			if err := subWt.AddUpdate(ctx); err != nil {
				return nil, fmt.Errorf(
					"submodule %s: git add -u: %w", p, err,
				)
			}
		}

		st, err := a.Worktree.SubmoduleStatus(ctx, p)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: status: %w", p, err,
			)
		}

		hasStaged, err := subWt.HasStagedChanges(ctx)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: check staged: %w", p, err,
			)
		}

		// State 3: staged content but on a different branch.
		// Detached + staged is treated as the same class.
		if hasStaged && (st.Detached || st.Branch != recorded) {
			cur := st.Branch
			if st.Detached {
				cur = "(detached HEAD)"
			}
			return nil, &DivergedFromRecordError{
				Path:           p,
				RecordedBranch: recorded,
				CurrentBranch:  cur,
			}
		}

		todo = append(todo, plan{
			path:         p,
			recorded:     recorded,
			status:       st,
			hasStaged:    hasStaged,
			shouldCommit: hasStaged,
		})
	}

	// Second pass: perform sub commits (state 1) and stage gitlinks.
	for _, t := range todo {
		subWt, err := a.Worktree.SubmoduleWorktree(ctx, t.path)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: open worktree: %w", t.path, err,
			)
		}

		if t.shouldCommit {
			subMsg := msg.Message
			if v, ok := msg.ModuleMessage[t.path]; ok {
				subMsg = v
			}
			req := git.CommitRequest{
				Message:     subMsg,
				MessageFile: msg.MessageFile,
				NoEdit:      msg.NoEdit && subMsg == "" && msg.MessageFile == "",
				NoVerify:    msg.NoVerify,
				Signoff:     msg.Signoff,
			}
			switch mode {
			case CommitModeAmend:
				req.Amend = true
				// In non-interactive amend with no new message, --no-edit.
				if subMsg == "" && msg.MessageFile == "" {
					req.NoEdit = true
				}
			case CommitModeCreate:
				// Plain commit; no special flags.
			}
			if err := subWt.Commit(ctx, req); err != nil {
				return nil, fmt.Errorf(
					"submodule %s: commit: %w", t.path, err,
				)
			}
		}

		// Recompute sub HEAD after potential commit.
		newHead, err := a.Worktree.SubmoduleHead(ctx, t.path)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: head: %w", t.path, err,
			)
		}

		// State 2 / post-commit: stage gitlink if HEAD moved past
		// the gitlink in parent's HEAD.
		if newHead != t.status.GitlinkHash {
			if err := a.parentUpdateGitlink(ctx, t.path, newHead); err != nil {
				return nil, fmt.Errorf(
					"submodule %s: stage gitlink: %w", t.path, err,
				)
			}
			staged = append(staged, t.path)
		}
	}

	return staged, nil
}

// PostAmendInteractiveSubmodules is the gitlink-only variant for the
// interactive `gs ca` editor-aborts-allowed path: state 1 is skipped
// (we cannot atomically amend subs across an editor abort), but state
// 2 (sub HEAD moved without our intervention) is honored.
//
// State 3 still surfaces as a DivergedFromRecordError so the parent's
// recorded gitlink does not silently disagree with the sub's branch.
func (a *Applier) PostAmendInteractiveSubmodules(
	ctx context.Context, parentBranch string,
) (staged []string, err error) {
	resp, err := a.Store.LookupBranch(ctx, parentBranch)
	if err != nil {
		if isErrNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"lookup branch %s: %w", parentBranch, err,
		)
	}
	if len(resp.Submodules) == 0 {
		return nil, nil
	}

	paths := make([]string, 0, len(resp.Submodules))
	for p := range resp.Submodules {
		if a.isExcluded(p) {
			continue
		}
		paths = append(paths, p)
	}
	slices.Sort(paths)

	for _, p := range paths {
		recorded := resp.Submodules[p]
		st, err := a.Worktree.SubmoduleStatus(ctx, p)
		if err != nil {
			return nil, fmt.Errorf(
				"submodule %s: status: %w", p, err,
			)
		}
		if st.HeadHash == st.GitlinkHash {
			continue
		}
		// Sub HEAD moved. Refuse to record a stale gitlink if the sub
		// is not on the recorded branch.
		if st.Detached || st.Branch != recorded {
			cur := st.Branch
			if st.Detached {
				cur = "(detached HEAD)"
			}
			return nil, &DivergedFromRecordError{
				Path:           p,
				RecordedBranch: recorded,
				CurrentBranch:  cur,
			}
		}
		if err := a.parentUpdateGitlink(ctx, p, st.HeadHash); err != nil {
			return nil, fmt.Errorf(
				"submodule %s: stage gitlink: %w", p, err,
			)
		}
		staged = append(staged, p)
	}
	return staged, nil
}

// parentUpdateGitlink stages a new gitlink in the parent worktree's
// index for the submodule at path.
func (a *Applier) parentUpdateGitlink(
	ctx context.Context, path string, head git.Hash,
) error {
	return a.Worktree.UpdateSubmodulePointer(ctx, path, head)
}

// isErrNotExist reports whether err indicates that a branch was not
// found in the state store.
func isErrNotExist(err error) bool {
	return errors.Is(err, state.ErrNotExist)
}
