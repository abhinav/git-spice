package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type reviewListCmd struct {
	Branch     string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to list comments for. Defaults to the current branch."`
	DraftOnly  bool   `name:"draft-only" help:"Show only draft comments."`
	Unresolved bool   `help:"Show only unresolved comments."`
	JSON       bool   `name:"json" released:"unreleased" help:"Write to stdout as a stream of JSON objects."`
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
	log *silog.Logger,
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
		Branch:     cmd.Branch,
		DraftOnly:  cmd.DraftOnly,
		Unresolved: cmd.Unresolved,
	})
	if err != nil {
		return err
	}

	if cmd.JSON {
		return writeReviewListJSON(kctx.Stdout, response)
	}
	writeReviewListText(log, response, cmd.DraftOnly)
	return nil
}

// writeReviewListText presents review comments in a human-readable form.
func writeReviewListText(
	log *silog.Logger,
	response *review.LoadResult,
	draftOnly bool,
) {
	if len(response.Drafts) > 0 {
		log.Infof("Draft comments:")
		for _, draft := range response.Drafts {
			writeReviewDraftText(log, draft)
		}
	}

	if draftOnly && len(response.Drafts) == 0 {
		log.Infof("No draft comments for %s.", response.Branch)
		return
	}

	if len(response.Comments) > 0 {
		log.Infof("Comments:")
		for _, comment := range response.Comments {
			writeForgeReviewCommentText(log, comment)
		}
	}

	if len(response.Comments) == 0 && len(response.Drafts) == 0 {
		log.Infof("No comments on %s.", response.Branch)
	}
}

func writeReviewDraftText(log *silog.Logger, draft review.Draft) {
	location := ""
	if draft.ReplyTo != "" {
		location = "reply:" + draft.ReplyTo
	} else {
		location = draft.Anchor.String()
	}
	log.Infof("  %-4s %s", draft.ID, location)
	writeReviewBody(log, draft.Body)
}

func writeForgeReviewCommentText(
	log *silog.Logger,
	comment review.ListedComment,
) {
	location := fmt.Sprintf(
		"%s:%d",
		comment.Thread.Path,
		comment.Thread.Range.StartLine,
	)
	threadInfo := ""
	if comment.Thread.ID != nil {
		threadInfo = " [" + comment.Thread.ID.String() + "]"
	}
	log.Infof(
		"  %-12s %s  %s  %s%s",
		comment.Comment.ID.String(),
		location,
		comment.Comment.Author,
		reviewCommentStatus(comment),
		threadInfo,
	)
	writeReviewBody(log, comment.Comment.Body)
}

func writeReviewBody(log *silog.Logger, body string) {
	for line := range strings.SplitSeq(body, "\n") {
		log.Infof("    %s", line)
	}
}

func reviewCommentStatus(comment review.ListedComment) string {
	if comment.Thread.Outdated != nil && *comment.Thread.Outdated {
		return "outdated"
	}
	if comment.Thread.Resolved != nil && *comment.Thread.Resolved {
		return "resolved"
	}
	return "open"
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
		if err := enc.Encode(reviewDraftToJSON(draft)); err != nil {
			return fmt.Errorf("encode draft: %w", err)
		}
	}
	for _, comment := range response.Comments {
		if err := enc.Encode(forgeReviewCommentToJSON(comment)); err != nil {
			return fmt.Errorf("encode forge: %w", err)
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
		if !draft.Anchor.IsLine() {
			comment.Range = &jsonCommentRange{
				Start: draft.Anchor.StartLine,
				End:   draft.Anchor.EndLine,
			}
		}
	}
	return comment
}

func forgeReviewCommentToJSON(comment review.ListedComment) jsonComment {
	var createdAt *time.Time
	if !comment.Comment.CreatedAt.IsZero() {
		createdAt = &comment.Comment.CreatedAt
	}

	scope := "line"
	if comment.Thread.Range.IsZero() {
		scope = "file"
	}
	result := jsonComment{
		Kind:      "forge",
		ID:        comment.Comment.ID.String(),
		Scope:     scope,
		Path:      comment.Thread.Path,
		Line:      comment.Thread.Range.StartLine,
		CommitSHA: comment.Thread.CommitHash.String(),
		Body:      comment.Comment.Body,
		ThreadID:  comment.Thread.ID.String(),
		Author:    comment.Comment.Author,
		Resolved:  comment.Thread.Resolved,
		Stale:     comment.Thread.Outdated,
		Status:    reviewCommentStatus(comment),
		CreatedAt: createdAt,
	}
	if scope == "line" {
		result.Side = comment.Thread.Side.String()
		if comment.Thread.Range.StartLine != comment.Thread.Range.EndLine {
			result.Range = &jsonCommentRange{
				Start: comment.Thread.Range.StartLine,
				End:   comment.Thread.Range.EndLine,
			}
		}
	}
	return result
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
