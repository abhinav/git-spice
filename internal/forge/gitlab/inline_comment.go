package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

var (
	_ forge.WithInlineComments   = (*Repository)(nil)
	_ forge.WithThreadResolution = (*Repository)(nil)
	_ forge.WithCommentEdit      = (*Repository)(nil)
)

// ListInlineComments lists inline/review comments on a MR
// by fetching discussions that have position information.
func (r *Repository) ListInlineComments(
	ctx context.Context,
	id forge.ChangeID,
) ([]*forge.InlineComment, error) {
	mr := mustMR(id)

	opts := &gitlab.ListMergeRequestDiscussionsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	var comments []*forge.InlineComment
	for {
		discussions, resp, err := r.client.MergeRequestDiscussionList(
			ctx, r.repoID, mr.Number, opts,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"list discussions: %w", err,
			)
		}

		for _, disc := range discussions {
			if len(disc.Notes) == 0 {
				continue
			}
			// Only include discussions that are resolvable
			// (i.e., code review / diff discussions).
			root := disc.Notes[0]
			if !root.Resolvable {
				continue
			}

			for _, note := range disc.Notes {
				path := ""
				var line int
				if note.Position != nil {
					path = note.Position.NewPath
					if note.Position.NewLine != 0 {
						line = int(note.Position.NewLine)
					}
				}

				createdAt := time.Time{}
				if note.CreatedAt != nil {
					createdAt = *note.CreatedAt
				}

				comments = append(comments,
					&forge.InlineComment{
						ID: &MRComment{
							Number:   note.ID,
							MRNumber: mr.Number,
						},
						ThreadID: formatThreadID(
							disc.ID, mr.Number,
						),
						Path:      path,
						Line:      line,
						Body:      note.Body,
						Author:    note.Author.Username,
						Resolved:  root.Resolved,
						Outdated:  note.Position != nil && note.Position.NewPath == "",
						CreatedAt: createdAt,
					})
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = int64(resp.NextPage)
	}

	return comments, nil
}

// SubmitReview posts a batch of inline comments
// as individual discussions on a MR.
// GitLab does not have a native batch review API,
// so each comment is posted as a separate discussion.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.ReviewRequest,
) error {
	mr := mustMR(id)

	// Get diff refs for positioning.
	diffRefs, err := r.diffRefs(ctx, mr.Number)
	if err != nil {
		return err
	}

	for _, c := range req.Comments {
		if c.ThreadID != "" {
			// Reply to existing thread.
			discID, _, perr := parseThreadID(c.ThreadID)
			if perr != nil {
				return perr
			}
			body := c.Body
			if _, _, err := r.client.MergeRequestDiscussionNoteCreate(
				ctx, r.repoID, mr.Number, discID,
				&gitlab.AddMergeRequestDiscussionNoteOptions{
					Body: &body,
				},
			); err != nil {
				return fmt.Errorf(
					"reply to thread %s: %w",
					c.ThreadID, err,
				)
			}
			continue
		}

		opts := newDiscussionOptions(c.Body, c.Path, c.Line, diffRefs)
		if _, _, err := r.client.MergeRequestDiscussionCreate(
			ctx, r.repoID, mr.Number, opts,
		); err != nil {
			return fmt.Errorf(
				"create discussion on %s:%d: %w",
				c.Path, c.Line, err,
			)
		}
	}

	r.log.Debug("Submitted review",
		"mr", mr.Number,
		"comments", len(req.Comments),
	)
	return nil
}

// PostInlineComment posts a single inline comment
// on a MR as a new discussion.
func (r *Repository) PostInlineComment(
	ctx context.Context,
	id forge.ChangeID,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	mr := mustMR(id)

	// Reply to existing thread.
	if req.ThreadID != "" {
		discID, _, err := parseThreadID(req.ThreadID)
		if err != nil {
			return nil, err
		}
		body := req.Body
		note, _, err := r.client.MergeRequestDiscussionNoteCreate(
			ctx, r.repoID, mr.Number, discID,
			&gitlab.AddMergeRequestDiscussionNoteOptions{
				Body: &body,
			},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"reply to thread: %w", err,
			)
		}

		createdAt := time.Time{}
		if note.CreatedAt != nil {
			createdAt = *note.CreatedAt
		}
		return &forge.InlineComment{
			ID: &MRComment{
				Number:   note.ID,
				MRNumber: mr.Number,
			},
			ThreadID:  req.ThreadID,
			Path:      req.Path,
			Line:      req.Line,
			Body:      req.Body,
			CreatedAt: createdAt,
		}, nil
	}

	// New discussion.
	diffRefs, err := r.diffRefs(ctx, mr.Number)
	if err != nil {
		return nil, err
	}

	disc, _, err := r.client.MergeRequestDiscussionCreate(
		ctx, r.repoID, mr.Number,
		newDiscussionOptions(req.Body, req.Path, req.Line, diffRefs),
	)
	if err != nil {
		return nil, fmt.Errorf("create discussion: %w", err)
	}

	var noteID int64
	if len(disc.Notes) > 0 {
		noteID = disc.Notes[0].ID
	}

	r.log.Debug("Posted inline comment",
		"mr", mr.Number,
		"path", req.Path,
		"line", req.Line,
	)

	return &forge.InlineComment{
		ID: &MRComment{
			Number:   noteID,
			MRNumber: mr.Number,
		},
		ThreadID: formatThreadID(disc.ID, mr.Number),
		Path:     req.Path,
		Line:     req.Line,
		Body:     req.Body,
	}, nil
}

func newDiscussionOptions(
	body, path string, line int,
	refs *gitlab.MergeRequestDiffRefs,
) *gitlab.CreateMergeRequestDiscussionOptions {
	textType := "text"
	lineN := int64(line)
	return &gitlab.CreateMergeRequestDiscussionOptions{
		Body: &body,
		Position: &gitlab.PositionOptions{
			BaseSHA:      &refs.BaseSha,
			HeadSHA:      &refs.HeadSha,
			StartSHA:     &refs.StartSha,
			PositionType: &textType,
			NewPath:      &path,
			OldPath:      &path,
			NewLine:      &lineN,
		},
	}
}

// diffRefs fetches the diff refs for a MR,
// needed for positioning inline comments.
func (r *Repository) diffRefs(
	ctx context.Context,
	mrNumber int64,
) (*gitlab.MergeRequestDiffRefs, error) {
	mr, _, err := r.client.MergeRequestGet(
		ctx, r.repoID, mrNumber, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}
	return &mr.DiffRefs, nil
}

// ResolveThread marks a discussion thread as resolved.
func (r *Repository) ResolveThread(
	ctx context.Context,
	threadID string,
) error {
	// Thread ID format: "discussion_id:mr_number"
	discID, mrNumber, err := parseThreadID(threadID)
	if err != nil {
		return err
	}

	resolved := true
	if _, _, err := r.client.MergeRequestDiscussionResolve(
		ctx, r.repoID, mrNumber, discID,
		&gitlab.ResolveMergeRequestDiscussionOptions{
			Resolved: &resolved,
		},
	); err != nil {
		return fmt.Errorf("resolve thread: %w", err)
	}

	r.log.Debug("Resolved thread", "threadID", threadID)
	return nil
}

// UnresolveThread marks a discussion thread as unresolved.
func (r *Repository) UnresolveThread(
	ctx context.Context,
	threadID string,
) error {
	discID, mrNumber, err := parseThreadID(threadID)
	if err != nil {
		return err
	}

	resolved := false
	if _, _, err := r.client.MergeRequestDiscussionResolve(
		ctx, r.repoID, mrNumber, discID,
		&gitlab.ResolveMergeRequestDiscussionOptions{
			Resolved: &resolved,
		},
	); err != nil {
		return fmt.Errorf("unresolve thread: %w", err)
	}

	r.log.Debug("Unresolved thread", "threadID", threadID)
	return nil
}

// EditComment updates the body of an existing MR comment.
func (r *Repository) EditComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	cid := mustMRComment(id)

	// Find the discussion containing the note
	// so we can use the discussion-level update API.
	discID, err := r.findDiscussionForNote(
		ctx, cid.MRNumber, cid.Number,
	)
	if err != nil {
		return err
	}

	if _, _, err := r.client.MergeRequestDiscussionNoteUpdate(
		ctx, r.repoID, cid.MRNumber, discID, cid.Number,
		&gitlab.UpdateMergeRequestDiscussionNoteOptions{
			Body: &body,
		},
	); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}

	r.log.Debug("Edited comment", "noteID", cid.Number)
	return nil
}

// findDiscussionForNote finds the discussion ID
// that contains a specific note.
func (r *Repository) findDiscussionForNote(
	ctx context.Context,
	mrNumber, noteID int64,
) (string, error) {
	opts := &gitlab.ListMergeRequestDiscussionsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	for {
		discussions, resp, err := r.client.MergeRequestDiscussionList(
			ctx, r.repoID, mrNumber, opts,
		)
		if err != nil {
			return "", fmt.Errorf(
				"list discussions: %w", err,
			)
		}

		for _, disc := range discussions {
			for _, note := range disc.Notes {
				if note.ID == noteID {
					return disc.ID, nil
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = int64(resp.NextPage)
	}

	return "", fmt.Errorf(
		"note %d not found in MR %d", noteID, mrNumber,
	)
}

// threadID format for GitLab: "discussion_id:mr_number"
// This encodes both the discussion ID and MR number
// so resolve/unresolve can work without extra context.

func formatThreadID(discID string, mrNumber int64) string {
	return discID + ":" + strconv.FormatInt(mrNumber, 10)
}

func parseThreadID(threadID string) (
	discID string, mrNumber int64, err error,
) {
	for i := len(threadID) - 1; i >= 0; i-- {
		if threadID[i] == ':' {
			discID = threadID[:i]
			mrNumber, err = strconv.ParseInt(
				threadID[i+1:], 10, 64,
			)
			if err != nil {
				return "", 0, fmt.Errorf(
					"parse thread ID %q: %w",
					threadID, err,
				)
			}
			return discID, mrNumber, nil
		}
	}
	return "", 0, fmt.Errorf(
		"invalid thread ID format: %q", threadID,
	)
}
