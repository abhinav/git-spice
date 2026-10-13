package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewListCmd struct {
	Branch     string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to list comments for. Defaults to the current branch."`
	DraftOnly  bool   `name:"draft-only" help:"Show only draft comments."`
	Unresolved bool   `help:"Show only unresolved comments."`
	JSON       bool   `name:"json" help:"Write to stdout as a stream of JSON objects."`
}

func (*reviewListCmd) Help() string {
	return text.Dedent(`
		Lists comments on the change request
		associated with the current branch.
		Use --branch to target a different branch.

		Draft comments are identified by a branch-local integer.

		Use --draft-only to show only draft comments.
		Use --unresolved to show only unresolved comments.

		With --json, prints output to stdout
		as a stream of JSON objects.
	`)
}

func (cmd *reviewListCmd) Run(
	ctx context.Context,
	kctx *kong.Context,
	wt *git.Worktree,
	handler ReviewHandler,
) error {
	if cmd.Branch == "" {
		branch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = branch
	}

	response, err := handler.LoadReviewData(ctx, &review.LoadRequest{
		Branch:              cmd.Branch,
		DraftOnly:           cmd.DraftOnly,
		Unresolved:          cmd.Unresolved,
		IncludeSource:       !cmd.JSON,
		IncludeReplyThreads: !cmd.JSON,
	})
	if err != nil {
		return err
	}

	if cmd.JSON {
		return writeReviewListJSON(kctx.Stdout, response)
	}
	return writeReviewListText(kctx.Stdout, response, cmd.DraftOnly)
}

// reviewListRenderer emits the human-readable review hierarchy.
// Root drafts and remote threads form separate sections,
// while draft replies stay inside the remote thread they target:
//
//	Draft comments
//
//	  1  main.go:4
//	    Use a helper here.
//
//	Review threads
//
//	  thread-100  main.go:5-12  [resolved]
//	    alice:
//	      This block is doing too much.
//	    draft 2:
//	      I will send a follow-up.
type reviewListRenderer struct {
	w         io.Writer
	branch    string
	draftOnly bool

	rootDrafts       []review.ListedDraft
	threads          []review.ListedThread
	repliesByThread  map[string][]review.ListedDraft
	replyThreadOrder []string
}

// writeReviewListText presents drafts and remote discussions as a hierarchy.
func writeReviewListText(
	w io.Writer,
	response *review.LoadResult,
	draftOnly bool,
) (retErr error) {
	bufw := bufio.NewWriter(w)
	defer func() {
		retErr = errors.Join(retErr, bufw.Flush())
	}()

	renderer := newReviewListRenderer(bufw, response, draftOnly)
	renderer.render()
	return nil
}

func newReviewListRenderer(
	w io.Writer,
	response *review.LoadResult,
	draftOnly bool,
) *reviewListRenderer {
	renderer := &reviewListRenderer{
		w:               w,
		branch:          response.Branch,
		draftOnly:       draftOnly,
		repliesByThread: make(map[string][]review.ListedDraft),
	}

	// Root drafts become independent list items. Reply drafts are indexed by
	// thread, with first-seen order retained for threads the forge no longer
	// returns.
	for _, draft := range response.Drafts {
		if draft.Draft.ReplyTo == "" {
			renderer.rootDrafts = append(renderer.rootDrafts, draft)
			continue
		}
		if _, ok := renderer.repliesByThread[draft.Draft.ReplyTo]; !ok {
			renderer.replyThreadOrder = append(
				renderer.replyThreadOrder,
				draft.Draft.ReplyTo,
			)
		}
		renderer.repliesByThread[draft.Draft.ReplyTo] = append(
			renderer.repliesByThread[draft.Draft.ReplyTo],
			draft,
		)
	}

	// Draft-only output retains a remote thread only when it gives context to
	// a local draft reply.
	for _, thread := range response.Threads {
		threadID := thread.Thread.ID.String()
		if draftOnly && len(renderer.repliesByThread[threadID]) == 0 {
			continue
		}
		renderer.threads = append(renderer.threads, thread)
	}
	return renderer
}

func (r *reviewListRenderer) render() {
	// Root drafts lead the document as standalone comments:
	//
	//	Draft comments
	//
	//	  1  main.go:4
	//	    Use a helper here.
	if len(r.rootDrafts) > 0 {
		fmt.Fprintln(r.w, "Draft comments")
		for _, draft := range r.rootDrafts {
			fmt.Fprintln(r.w)
			r.draft(draft)
		}
	}

	// Published comments and local reply drafts share a thread section.
	// Keeping the replies nested makes the thread ID visibly govern every
	// operation below it.
	if len(r.threads) > 0 || len(r.replyThreadOrder) > 0 {
		if len(r.rootDrafts) > 0 {
			fmt.Fprintln(r.w)
		}
		fmt.Fprintln(r.w, "Review threads")

		for _, thread := range r.threads {
			threadID := thread.Thread.ID.String()
			fmt.Fprintln(r.w)
			r.thread(thread, r.repliesByThread[threadID])
			delete(r.repliesByThread, threadID)
		}

		// A forge may stop returning a thread while a local reply still targets
		// it. Keep that draft operable under an unavailable thread header without
		// inventing an anchor or remote state.
		for _, threadID := range r.replyThreadOrder {
			replies := r.repliesByThread[threadID]
			if len(replies) == 0 {
				continue
			}
			fmt.Fprintln(r.w)
			fmt.Fprintf(r.w, "  %s  [unavailable]\n", threadID)
			for _, draft := range replies {
				r.reply(draft.Draft)
			}
		}
	}

	if len(r.rootDrafts) == 0 && len(r.threads) == 0 && len(r.replyThreadOrder) == 0 {
		if r.draftOnly {
			fmt.Fprintf(r.w, "No draft comments on %s.\n", r.branch)
		} else {
			fmt.Fprintf(r.w, "No review comments on %s.\n", r.branch)
		}
	}
}

func (r *reviewListRenderer) draft(listed review.ListedDraft) {
	draft := listed.Draft
	fmt.Fprintf(r.w, "  %v  %s\n", draft.ID, draft.Anchor)
	r.sourceExcerpt(
		listed.Source,
		draft.Anchor.StartLine,
		draft.Anchor.EndLine,
	)
	r.body("    ", draft.Body)
}

func (r *reviewListRenderer) thread(
	listed review.ListedThread,
	draftReplies []review.ListedDraft,
) {
	thread := listed.Thread
	fmt.Fprintf(
		r.w,
		"  %s  %s%s\n",
		thread.ID,
		reviewThreadAnchor(thread),
		reviewThreadState(thread),
	)
	r.sourceExcerpt(
		listed.Source,
		thread.Range.StartLine,
		thread.Range.EndLine,
	)
	if !r.draftOnly {
		for _, comment := range thread.Comments {
			author := comment.Author
			if author == "" {
				author = "comment"
			}
			fmt.Fprintf(r.w, "    %s:\n", author)
			r.body("      ", comment.Body)
		}
	}
	for _, draft := range draftReplies {
		r.reply(draft.Draft)
	}
}

func (r *reviewListRenderer) reply(draft review.Draft) {
	fmt.Fprintf(r.w, "    draft %v:\n", draft.ID)
	r.body("      ", draft.Body)
}

// sourceExcerpt marks selected lines and inserts an omission marker where a
// long selected range was abbreviated by the handler.
func (r *reviewListRenderer) sourceExcerpt(
	lines []review.SourceLine,
	selectedStart, selectedEnd int,
) {
	if len(lines) == 0 {
		return
	}

	width := len(strconv.Itoa(lines[len(lines)-1].Number))
	previous := lines[0].Number - 1
	for _, line := range lines {
		if line.Number > previous+1 {
			fmt.Fprintf(r.w, "      %*s | ...\n", width, "")
		}
		marker := " "
		if line.Number >= selectedStart && line.Number <= selectedEnd {
			marker = ">"
		}
		fmt.Fprintf(r.w, "    %s %*v |", marker, width, line.Number)
		if line.Text != "" {
			fmt.Fprintf(r.w, " %s", line.Text)
		}
		fmt.Fprintln(r.w)
		previous = line.Number
	}
}

func (r *reviewListRenderer) body(indent, body string) {
	for line := range strings.SplitSeq(body, "\n") {
		fmt.Fprintf(r.w, "%s%s\n", indent, line)
	}
}

// reviewThreadAnchor returns the command-facing anchor and calls out left-side
// coordinates because right-side coordinates are the default.
func reviewThreadAnchor(thread forge.ReviewThread) string {
	location := thread.Path
	if !thread.Range.IsZero() {
		if thread.Range.StartLine == thread.Range.EndLine {
			location += fmt.Sprintf(":%v", thread.Range.StartLine)
		} else {
			location += fmt.Sprintf(":%v-%v", thread.Range.StartLine, thread.Range.EndLine)
		}
		if thread.Side == forge.ReviewThreadSideLeft {
			location += " (left)"
		}
	}
	return location
}

// reviewThreadState reports only non-default states that affect scanning.
func reviewThreadState(thread forge.ReviewThread) string {
	var states []string
	if thread.Resolved != nil && *thread.Resolved {
		states = append(states, "resolved")
	}
	if thread.Outdated != nil && *thread.Outdated {
		states = append(states, "outdated")
	}
	if len(states) == 0 {
		return ""
	}
	return "  [" + strings.Join(states, ", ") + "]"
}

// writeReviewListJSON encodes review comments as NDJSON.
func writeReviewListJSON(
	w io.Writer,
	response *review.LoadResult,
) (retErr error) {
	bufw := bufio.NewWriter(w)
	defer func() {
		retErr = errors.Join(retErr, bufw.Flush())
	}()

	enc := json.NewEncoder(bufw)
	for _, draft := range response.Drafts {
		if err := enc.Encode(reviewDraftToJSON(draft.Draft)); err != nil {
			return fmt.Errorf("encode draft: %w", err)
		}
	}
	for _, thread := range response.Threads {
		for _, comment := range thread.Thread.Comments {
			if err := enc.Encode(forgeReviewCommentToJSON(thread.Thread, comment)); err != nil {
				return fmt.Errorf("encode forge: %w", err)
			}
		}
	}
	return nil
}

func reviewDraftToJSON(draft review.Draft) jsonComment {
	comment := jsonComment{
		Kind: "draft",
		ID:   draft.ID.String(),
		Body: draft.Body,
	}
	if draft.ReplyTo != "" {
		comment.ThreadID = draft.ReplyTo
		return comment
	}

	comment.Path = draft.Anchor.Path
	comment.Line = draft.Anchor.StartLine
	if draft.Anchor.IsFile() {
		comment.Scope = "file"
	} else {
		comment.Scope = "line"
		comment.Side = forge.ReviewThreadSideRight.String()
		if !draft.Anchor.IsLine() {
			comment.Range = &jsonCommentRange{
				Start: draft.Anchor.StartLine,
				End:   draft.Anchor.EndLine,
			}
		}
	}
	return comment
}

func forgeReviewCommentToJSON(
	thread forge.ReviewThread,
	comment forge.ReviewComment,
) jsonComment {
	var createdAt *time.Time
	if !comment.CreatedAt.IsZero() {
		createdAt = &comment.CreatedAt
	}

	scope := "line"
	if thread.Range.IsZero() {
		scope = "file"
	}
	result := jsonComment{
		Kind:      "forge",
		ID:        comment.ID.String(),
		Scope:     scope,
		Path:      thread.Path,
		Line:      thread.Range.StartLine,
		CommitSHA: thread.CommitHash.String(),
		Body:      comment.Body,
		ThreadID:  thread.ID.String(),
		Author:    comment.Author,
		Resolved:  thread.Resolved,
		Stale:     thread.Outdated,
		Status:    reviewCommentStatus(thread),
		CreatedAt: createdAt,
	}
	if scope == "line" {
		result.Side = thread.Side.String()
		if thread.Range.StartLine != thread.Range.EndLine {
			result.Range = &jsonCommentRange{
				Start: thread.Range.StartLine,
				End:   thread.Range.EndLine,
			}
		}
	}
	return result
}

func reviewCommentStatus(thread forge.ReviewThread) string {
	if thread.Outdated != nil && *thread.Outdated {
		return "outdated"
	}
	if thread.Resolved != nil && *thread.Resolved {
		return "resolved"
	}
	return "open"
}

// jsonComment is the JSON representation of a review comment.
type jsonComment struct {
	// Kind is "draft" or "forge".
	Kind string `json:"kind"`

	// ID is a branch-local integer for drafts and a forge ID otherwise.
	ID string `json:"id"`

	// Scope is "file" or "line".
	// Draft replies omit it because they inherit their thread's scope.
	Scope string `json:"scope,omitempty"`

	// Path is relative to the repository root.
	Path string `json:"path,omitempty"`

	// Line is the first line of a line-scoped comment.
	Line int `json:"line,omitempty"`

	// Range is set when a line comment spans multiple lines.
	Range *jsonCommentRange `json:"range,omitempty"`

	// Side is the diff side for a line comment.
	Side string `json:"side,omitempty"`

	// CommitSHA is the reviewed revision that owns the thread.
	CommitSHA string `json:"commitSHA,omitempty"`

	// Body is the full Markdown comment body.
	Body string `json:"body"`

	// ThreadID identifies the owning forge thread.
	ThreadID string `json:"threadID,omitempty"`

	// Author is set for forge comments.
	Author string `json:"author,omitempty"`

	// Resolved is omitted when the forge does not expose resolution state.
	Resolved *bool `json:"resolved,omitempty"`

	// Stale is omitted when the forge does not expose outdated state.
	Stale *bool `json:"stale,omitempty"`

	// Status is "open", "resolved", or "outdated" for forge comments.
	Status string `json:"status,omitempty"`

	// CreatedAt is set for forge comments with a creation timestamp.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// jsonCommentRange is an inclusive multi-line range in JSON output.
type jsonCommentRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
