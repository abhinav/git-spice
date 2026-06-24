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
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCommentListCmd struct {
	Branch     string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to list comments for. Defaults to current branch."`
	Staged     bool   `help:"Show only staged comments."`
	Unresolved bool   `help:"Show only unresolved comments."`
	JSON       bool   `name:"json" released:"unreleased" help:"Write to stdout as a stream of JSON objects."`
}

func (*branchCommentListCmd) Help() string {
	return text.Dedent(`
		Lists comments on the change request
		associated with the current branch.
		Use --branch to target a different branch.

		Staged comments that have not yet been submitted
		are shown with an 'sc-N' prefix.

		Use --staged to show only staged comments.
		Use --unresolved to show only unresolved comments.

		With --json, prints output to stdout
		as a stream of JSON objects.
	`)
}

func (cmd *branchCommentListCmd) Run(
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

func (cmd *branchCommentListCmd) resolveBranch(
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

func (cmd *branchCommentListCmd) loadComments(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	store *state.Store,
	forgeRepo forge.Repository,
	branch string,
) ([]*state.StagedComment, []*forge.InlineComment, error) {
	staged, err := loadStagedComments(ctx, store, branch)
	if err != nil {
		return nil, nil, err
	}

	if cmd.Staged {
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
		return nil, fmt.Errorf(
			"load staged comments: %w", err,
		)
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
) ([]*forge.InlineComment, error) {
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

	inline, ok := forgeRepo.(forge.WithInlineComments)
	if !ok {
		log.Infof(
			"Forge does not support inline comments.",
		)
		return nil, nil
	}

	comments, err := inline.ListInlineComments(
		ctx, b.Change.ChangeID(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list inline comments: %w", err,
		)
	}
	return comments, nil
}

func (cmd *branchCommentListCmd) filterForge(
	comments []*forge.InlineComment,
) []*forge.InlineComment {
	if !cmd.Unresolved {
		return comments
	}

	var filtered []*forge.InlineComment
	for _, c := range comments {
		if !c.Resolved {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// writeText prints comments in human-readable format.
func (cmd *branchCommentListCmd) writeText(
	log *silog.Logger,
	branch string,
	staged []*state.StagedComment,
	forgeComments []*forge.InlineComment,
) error {
	if len(staged) > 0 {
		log.Infof("Staged comments:")
		for _, c := range staged {
			writeStagedText(log, c)
		}
	}

	if cmd.Staged && len(staged) == 0 {
		log.Infof("No staged comments for %s.", branch)
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
	log.Infof("  sc-%-4d %s", c.ID, location)
	writeBodyIndented(log, c.Body)
}

func writeForgeText(
	log *silog.Logger, c *forge.InlineComment,
) {
	location := fmt.Sprintf("%s:%d", c.Path, c.Line)
	threadInfo := ""
	if c.ThreadID != "" {
		threadInfo = " [" + c.ThreadID + "]"
	}
	log.Infof(
		"  %-12s %s  %s  %s%s",
		c.ID, location, c.Author,
		commentStatus(c), threadInfo,
	)
	writeBodyIndented(log, c.Body)
}

func writeBodyIndented(log *silog.Logger, body string) {
	for line := range strings.SplitSeq(body, "\n") {
		log.Infof("    %s", line)
	}
}

func commentStatus(c *forge.InlineComment) string {
	if c.Outdated {
		return "outdated"
	}
	if c.Resolved {
		return "resolved"
	}
	return "open"
}

// writeJSON encodes comments as NDJSON to stdout.
func (cmd *branchCommentListCmd) writeJSON(
	w io.Writer,
	staged []*state.StagedComment,
	forgeComments []*forge.InlineComment,
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
	return jsonComment{
		Kind:     "staged",
		ID:       fmt.Sprintf("sc-%d", c.ID),
		Path:     c.File,
		Line:     c.Line,
		Body:     c.Body,
		ThreadID: c.ThreadID,
	}
}

func forgeToJSON(c *forge.InlineComment) jsonComment {
	var createdAt *time.Time
	if !c.CreatedAt.IsZero() {
		createdAt = &c.CreatedAt
	}
	return jsonComment{
		Kind:      "forge",
		ID:        c.ID.String(),
		Path:      c.Path,
		Line:      c.Line,
		Body:      c.Body,
		ThreadID:  c.ThreadID,
		Author:    c.Author,
		Status:    commentStatus(c),
		CreatedAt: createdAt,
	}
}

// jsonComment is the JSON representation
// of a comment for --json output.
type jsonComment struct {
	// Kind is "staged" or "forge".
	Kind string `json:"kind"`

	// ID is the comment identifier.
	// For staged comments: "sc-N".
	// For forge comments: forge-specific ID.
	ID string `json:"id"`

	// Path is the file path relative to the repo root.
	Path string `json:"path,omitempty"`

	// Line is the line number in the file.
	Line int `json:"line,omitempty"`

	// Body is the full markdown body of the comment.
	Body string `json:"body"`

	// ThreadID is the thread identifier, if any.
	ThreadID string `json:"threadID,omitempty"`

	// Author is the username of the comment author.
	// Only set for forge comments.
	Author string `json:"author,omitempty"`

	// Status is "open", "resolved", or "outdated".
	// Only set for forge comments.
	Status string `json:"status,omitempty"`

	// CreatedAt is the time the comment was created.
	// Only set for forge comments.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}
