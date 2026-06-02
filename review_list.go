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
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
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
	svc *spice.Service,
	store *state.Store,
	forgeRepo forge.Repository,
) error {
	branch, err := cmd.resolveBranch(ctx, wt)
	if err != nil {
		return err
	}

	staged, forgeComments, err := cmd.loadComments(
		ctx, log, svc, store, forgeRepo, branch,
	)
	if err != nil {
		return err
	}

	if cmd.JSON {
		return cmd.writeJSON(
			kctx.Stdout, staged, forgeComments,
		)
	}
	return cmd.writeText(log, branch, staged, forgeComments)
}

func (cmd *reviewListCmd) resolveBranch(
	ctx context.Context, wt *git.Worktree,
) (string, error) {
	if cmd.Branch != "" {
		return cmd.Branch, nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return branch, nil
}

func (cmd *reviewListCmd) loadComments(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	store *state.Store,
	forgeRepo forge.Repository,
	branch string,
) ([]*state.StagedComment, []*listedReviewComment, error) {
	staged, err := loadStagedComments(ctx, store, branch)
	if err != nil {
		return nil, nil, err
	}

	if cmd.DraftOnly {
		return staged, nil, nil
	}

	forgeComments, err := loadForgeComments(
		ctx, log, svc, forgeRepo, branch,
	)
	if err != nil {
		return nil, nil, err
	}

	return staged, cmd.filterForge(forgeComments), nil
}

func loadStagedComments(
	ctx context.Context,
	store *state.Store,
	branch string,
) ([]*state.StagedComment, error) {
	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("load draft comments: %w", err)
	}
	if staged == nil {
		return nil, nil
	}

	refs := make(
		[]*state.StagedComment, len(staged.Comments),
	)
	for i := range staged.Comments {
		refs[i] = &staged.Comments[i]
	}
	return refs, nil
}

func loadForgeComments(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	forgeRepo forge.Repository,
	branch string,
) ([]*listedReviewComment, error) {
	b, err := svc.LookupBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil, fmt.Errorf(
				"branch not tracked: %s", branch,
			)
		}
		return nil, fmt.Errorf("get branch: %w", err)
	}

	if b.Change == nil {
		log.Infof(
			"No change request found for %s.", branch,
		)
		return nil, nil
	}

	reviewRepo, ok := forgeRepo.(forge.ReviewRepository)
	if !ok {
		log.Infof(
			"Forge does not support review comments.",
		)
		return nil, nil
	}

	threads, err := sliceutil.CollectErr(
		reviewRepo.ListReviewThreads(ctx, b.Change.ChangeID()),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list review threads: %w", err,
		)
	}

	var comments []*listedReviewComment
	for _, thread := range threads {
		for i := range thread.Comments {
			comments = append(comments, &listedReviewComment{
				Thread:  thread,
				Comment: &thread.Comments[i],
			})
		}
	}
	return comments, nil
}

// listedReviewComment keeps a comment paired with the thread that owns its
// location and status.
type listedReviewComment struct {
	Thread  *forge.ReviewThread
	Comment *forge.ReviewComment
}

func (cmd *reviewListCmd) filterForge(
	comments []*listedReviewComment,
) []*listedReviewComment {
	if !cmd.Unresolved {
		return comments
	}

	var filtered []*listedReviewComment
	for _, c := range comments {
		if c.Thread.Resolved == nil || !*c.Thread.Resolved {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// writeText prints comments in human-readable format.
func (cmd *reviewListCmd) writeText(
	log *silog.Logger,
	branch string,
	staged []*state.StagedComment,
	forgeComments []*listedReviewComment,
) error {
	if len(staged) > 0 {
		log.Infof("Draft comments:")
		for _, c := range staged {
			writeStagedText(log, c)
		}
	}

	if cmd.DraftOnly && len(staged) == 0 {
		log.Infof("No draft comments for %s.", branch)
		return nil
	}

	if len(forgeComments) > 0 {
		log.Infof("Comments:")
		for _, c := range forgeComments {
			writeForgeText(log, c)
		}
	}

	if len(forgeComments) == 0 && len(staged) == 0 {
		log.Infof("No comments on %s.", branch)
	}
	return nil
}

func writeStagedText(
	log *silog.Logger, c *state.StagedComment,
) {
	location := fmt.Sprintf("%s:%d", c.File, c.Line)
	if c.ThreadID != "" {
		location = "reply:" + c.ThreadID
	}
	log.Infof("  %-4d %s", c.ID, location)
	writeBodyIndented(log, c.Body)
}

func writeForgeText(
	log *silog.Logger, c *listedReviewComment,
) {
	location := fmt.Sprintf(
		"%s:%d", c.Thread.Path, c.Thread.Range.StartLine,
	)
	threadInfo := ""
	if c.Thread.ID != nil {
		threadInfo = " [" + c.Thread.ID.String() + "]"
	}
	log.Infof(
		"  %-12s %s  %s  %s%s",
		c.Comment.ID.String(), location, c.Comment.Author,
		commentStatus(c), threadInfo,
	)
	writeBodyIndented(log, c.Comment.Body)
}

func writeBodyIndented(log *silog.Logger, body string) {
	for line := range strings.SplitSeq(body, "\n") {
		log.Infof("    %s", line)
	}
}

func commentStatus(c *listedReviewComment) string {
	if c.Thread.Outdated != nil && *c.Thread.Outdated {
		return "outdated"
	}
	if c.Thread.Resolved != nil && *c.Thread.Resolved {
		return "resolved"
	}
	return "open"
}

// writeJSON encodes comments as NDJSON to stdout.
func (cmd *reviewListCmd) writeJSON(
	w io.Writer,
	staged []*state.StagedComment,
	forgeComments []*listedReviewComment,
) (retErr error) {
	bufw := bufio.NewWriter(w)
	defer func() {
		retErr = errors.Join(retErr, bufw.Flush())
	}()

	enc := json.NewEncoder(bufw)
	for _, c := range staged {
		if err := enc.Encode(stagedToJSON(c)); err != nil {
			return fmt.Errorf("encode staged: %w", err)
		}
	}
	for _, c := range forgeComments {
		if err := enc.Encode(forgeToJSON(c)); err != nil {
			return fmt.Errorf("encode forge: %w", err)
		}
	}
	return nil
}

func stagedToJSON(c *state.StagedComment) jsonComment {
	comment := jsonComment{
		Kind:     "draft",
		ID:       fmt.Sprintf("%d", c.ID),
		Body:     c.Body,
		ThreadID: c.ThreadID,
	}
	if c.ThreadID == "" {
		comment.Scope = "line"
		comment.Path = c.File
		comment.Line = c.Line
	}
	return comment
}

func forgeToJSON(c *listedReviewComment) jsonComment {
	var createdAt *time.Time
	if !c.Comment.CreatedAt.IsZero() {
		createdAt = &c.Comment.CreatedAt
	}

	scope := "line"
	if c.Thread.Range.IsZero() {
		scope = "file"
	}
	comment := jsonComment{
		Kind:      "forge",
		ID:        c.Comment.ID.String(),
		Scope:     scope,
		Path:      c.Thread.Path,
		Line:      c.Thread.Range.StartLine,
		CommitSHA: c.Thread.CommitHash.String(),
		Body:      c.Comment.Body,
		ThreadID:  c.Thread.ID.String(),
		Author:    c.Comment.Author,
		Resolved:  c.Thread.Resolved,
		Stale:     c.Thread.Outdated,
		Status:    commentStatus(c),
		CreatedAt: createdAt,
	}
	if scope == "line" {
		comment.Side = c.Thread.Side.String()
		if c.Thread.Range.StartLine != c.Thread.Range.EndLine {
			comment.Range = &jsonCommentRange{
				Start: c.Thread.Range.StartLine,
				End:   c.Thread.Range.EndLine,
			}
		}
	}
	return comment
}

// jsonComment is the JSON representation
// of a comment for --json output.
type jsonComment struct {
	// Kind is "draft" or "forge".
	Kind string `json:"kind"`

	// ID is the comment identifier.
	// For draft comments: a branch-local integer encoded as a string.
	// For forge comments: forge-specific ID.
	ID string `json:"id"`

	// Scope is "file" or "line".
	// It is omitted for draft replies, which inherit their thread's scope.
	Scope string `json:"scope,omitempty"`

	// Path is the file path relative to the repo root.
	Path string `json:"path,omitempty"`

	// Line is the line number in the file.
	Line int `json:"line,omitempty"`

	// Range is set when a line comment spans more than one line.
	Range *jsonCommentRange `json:"range,omitempty"`

	// Side is the diff side for a line comment.
	Side string `json:"side,omitempty"`

	// CommitSHA is the reviewed revision against which the thread was created.
	// It is empty when the forge does not expose that revision.
	CommitSHA string `json:"commitSHA,omitempty"`

	// Body is the full markdown body of the comment.
	Body string `json:"body"`

	// ThreadID is the thread identifier, if any.
	ThreadID string `json:"threadID,omitempty"`

	// Author is the username of the comment author.
	// Only set for forge comments.
	Author string `json:"author,omitempty"`

	// Resolved reports whether the thread is resolved.
	// It is omitted when the forge does not expose resolution state.
	Resolved *bool `json:"resolved,omitempty"`

	// Stale reports whether the thread belongs to an earlier revision.
	// It is omitted when the forge does not expose outdated state.
	Stale *bool `json:"stale,omitempty"`

	// Status is "open", "resolved", or "outdated".
	// Only set for forge comments.
	Status string `json:"status,omitempty"`

	// CreatedAt is the time the comment was created.
	// Only set for forge comments.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// jsonCommentRange is an inclusive multi-line range in JSON output.
type jsonCommentRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
