package review

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
)

// LoadRequest selects review comments to load.
type LoadRequest struct {
	// Branch identifies the reviewed branch.
	Branch string // required

	// DraftOnly omits comments already published to the forge.
	DraftOnly bool

	// Unresolved omits resolved forge threads.
	Unresolved bool

	// IncludeSource adds best-effort source excerpts to loaded anchors.
	IncludeSource bool

	// IncludeReplyThreads loads thread metadata needed to place draft replies
	// when DraftOnly is set.
	IncludeReplyThreads bool
}

// LoadResult contains local drafts and published review threads.
type LoadResult struct {
	// Branch is the branch selected by the request.
	Branch string

	// Drafts are the branch's unpublished comments.
	Drafts []ListedDraft

	// Threads are published review discussions.
	Threads []ListedThread
}

// LoadReviewData loads local drafts and remote review threads for a branch.
func (h *Handler) LoadReviewData(
	ctx context.Context,
	req *LoadRequest,
) (*LoadResult, error) {
	drafts, err := h.Store.LoadReviewDrafts(ctx, req.Branch)
	if err != nil {
		return nil, fmt.Errorf("load draft comments: %w", err)
	}
	result := &LoadResult{Branch: req.Branch}
	for _, draft := range drafts {
		listed := ListedDraft{Draft: draft}
		if req.IncludeSource && draft.ReplyTo == "" && !draft.Anchor.IsFile() {
			listed.Source = h.loadSource(
				ctx,
				draft.CommitHash,
				draft.Anchor.Path,
				draft.Anchor.StartLine,
				draft.Anchor.EndLine,
			)
		}
		result.Drafts = append(result.Drafts, listed)
	}

	draftReplyTargets := make(map[string]struct{})
	for _, draft := range drafts {
		if draft.ReplyTo != "" {
			draftReplyTargets[draft.ReplyTo] = struct{}{}
		}
	}
	// Draft replies need their remote thread even in --draft-only output.
	if req.DraftOnly && (!req.IncludeReplyThreads || len(draftReplyTargets) == 0) {
		return result, nil
	}

	change, err := h.Service.LookupBranch(ctx, req.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil, fmt.Errorf("branch not tracked: %s", req.Branch)
		}
		return nil, fmt.Errorf("get branch: %w", err)
	}
	if change.Change == nil {
		h.Log.Infof("No change request found for %s.", req.Branch)
		return result, nil
	}

	for thread, err := range h.Repository.ListReviewThreads(
		ctx,
		change.Change.ChangeID(),
	) {
		if err != nil {
			return nil, fmt.Errorf("list review threads: %w", err)
		}
		_, hasDraftReply := draftReplyTargets[thread.ID.String()]
		contextOnly := req.DraftOnly
		if req.Unresolved && thread.Resolved != nil && *thread.Resolved {
			if !hasDraftReply {
				continue
			}
			contextOnly = true
		}
		if req.DraftOnly && !hasDraftReply {
			continue
		}

		listed := ListedThread{Thread: *thread}
		if contextOnly {
			listed.Thread.Comments = nil
		}
		if req.IncludeSource && thread.Side == forge.ReviewThreadSideRight && !thread.Range.IsZero() {
			listed.Source = h.loadSource(
				ctx,
				thread.CommitHash,
				thread.Path,
				thread.Range.StartLine,
				thread.Range.EndLine,
			)
		}
		result.Threads = append(result.Threads, listed)
	}
	return result, nil
}

// ListedDraft is a local draft with optional source context.
type ListedDraft struct {
	// Draft is the stored local comment.
	Draft Draft

	// Source is the available source context around Draft's anchor.
	// It is empty for replies, file-level drafts, and unavailable source.
	Source []SourceLine
}

// ListedThread is a remote review thread with optional source context.
type ListedThread struct {
	// Thread is the forge discussion.
	Thread forge.ReviewThread

	// Source is the available source context around Thread's anchor.
	// It is empty for file-level and left-side threads and unavailable source.
	Source []SourceLine
}

// SourceLine is one line of source shown around a review anchor.
type SourceLine struct {
	// Number is the one-based line number in the source file.
	Number int

	// Text is the source line without its line ending.
	Text string
}

// loadSource returns a bounded excerpt when the anchor's source is available.
// Source context is supplementary, so any Git or scanning failure omits it
// without changing the review-list operation.
func (h *Handler) loadSource(
	ctx context.Context,
	commit git.Hash,
	path string,
	start, end int,
) []SourceLine {
	if commit.IsZero() {
		return nil
	}

	source, err := h.Worktree.OpenFileAt(ctx, commit, path)
	if err != nil {
		return nil
	}
	lines, scanErr := scanSourceExcerpt(source, start, end)
	var drainErr error
	if scanErr == nil {
		// The excerpt stops after trailing context. Drain the process pipe so
		// closing it early does not turn an otherwise usable excerpt into a
		// cat-file failure for a large source file.
		_, drainErr = io.Copy(io.Discard, source)
	}
	closeErr := source.Close()
	if errors.Join(scanErr, drainErr, closeErr) != nil {
		return nil
	}
	return lines
}

const (
	_reviewContextLines      = 3
	_reviewSelectedLineLimit = 6
)

// scanSourceExcerpt reads through the anchor and its trailing context without
// retaining the rest of the file. Long selected ranges keep their first and
// last three lines; the presenter marks the omitted interval.
func scanSourceExcerpt(
	r io.Reader,
	start, end int,
) ([]SourceLine, error) {
	first := max(1, start-_reviewContextLines)
	last := end + _reviewContextLines
	selectedLines := end - start + 1

	var lines []SourceLine
	scanner := bufio.NewScanner(r)
	lastRead := 0
	for lineNumber := 1; lineNumber <= last && scanner.Scan(); lineNumber++ {
		lastRead = lineNumber
		if lineNumber < first {
			continue
		}
		if selectedLines > _reviewSelectedLineLimit &&
			lineNumber >= start+_reviewSelectedLineLimit/2 &&
			lineNumber <= end-_reviewSelectedLineLimit/2 {
			continue
		}
		lines = append(lines, SourceLine{
			Number: lineNumber,
			Text:   scanner.Text(),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lastRead < end {
		return nil, nil
	}
	return lines, nil
}
