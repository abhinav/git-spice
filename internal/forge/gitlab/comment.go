package gitlab

import (
	"context"
	"fmt"
	"iter"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// MRComment identifies a comment on a GitLab MR.
// These are referred to as "notes" in GitLab's API.
//
// MRComment implements [forge.ChangeCommentID].
type MRComment struct {
	// Number is the ID of the note.
	Number int64 `json:"number"` // required

	// MRNumber is the ID of the MR the note is on.
	MRNumber int64 `json:"mr_number"` // required
}

var _ forge.ChangeCommentID = (*MRComment)(nil)

func mustMRComment(id forge.ChangeCommentID) *MRComment {
	if id == nil {
		return nil
	}

	mrc, ok := id.(*MRComment)
	if !ok {
		panic(fmt.Sprintf("unexpected MR comment type: %T", id))
	}
	return mrc
}

func (c *MRComment) String() string {
	return strconv.FormatInt(c.Number, 10)
}

// PostChangeComment posts a new comment on an MR.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	mr := mustMR(id)
	noteOptions := gitlab.CreateMergeRequestNoteOptions{
		Body: &markdown,
	}

	mrNumber := mr.Number
	note, _, err := r.client.Notes.CreateMergeRequestNote(
		r.repoID, mrNumber, &noteOptions,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	r.log.Debug("Posted comment", "id", note.ID, "mr", mrNumber)
	return &MRComment{
		Number:   note.ID,
		MRNumber: mrNumber,
	}, nil
}

// UpdateChangeComment updates the contents of an existing comment on an MR.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	mrComment := mustMRComment(id)
	noteOptions := gitlab.UpdateMergeRequestNoteOptions{
		Body: &markdown,
	}

	_, _, err := r.client.Notes.UpdateMergeRequestNote(
		r.repoID, mrComment.MRNumber, mrComment.Number, &noteOptions,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("update comment: %w", err)
	}
	r.log.Debug("Updated comment",
		"id", mrComment.Number,
		"mr", mrComment.MRNumber)

	return nil
}

// DeleteChangeComment deletes an existing comment on a PR.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	// DeleteChangeComment isn't part of the forge.Repository interface.
	// It's just nice to have to clean up after the integration test.
	mrComment := mustMRComment(id)

	_, err := r.client.Notes.DeleteMergeRequestNote(
		r.repoID, mrComment.MRNumber, mrComment.Number,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	r.log.Debug("Deleted comment", "id", mrComment.Number, "mr", mrComment.MRNumber)

	return nil
}

// There isn't a way to filter comments by contents server-side,
// so we'll be doing that client-side.
// GitLab's API paginates the notes listing endpoints so we will fetch them by 20 per page.
//
// Since our comment will usually be among the first few comments,
// that, plus the ascending order of comments, should make this good enough.
var _listChangeCommentsPageSize = 20 // var for testing

// ListChangeComments lists comments on an MR,
// optionally applying the given filtering options.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	var filters []func(gitlab.Note) (keep bool)
	if options != nil {
		if len(options.BodyMatchesAll) != 0 {
			for _, re := range options.BodyMatchesAll {
				filters = append(filters, func(node gitlab.Note) bool {
					return re.MatchString(node.Body)
				})
			}
		}

		// GitLab's API does not tell you whether you can update a comment.
		// This can be inferred based on the current user's role, though.
		// Per https://docs.gitlab.com/ee/user/discussions/#edit-a-comment,
		//
		// > You can edit your own comment at any time.
		// > Anyone with at least the Maintainer role can also edit a comment made by someone else.
		if options.CanUpdate {
			filters = append(filters, func(note gitlab.Note) bool {
				return note.Author.ID == r.userID || r.userRole >= gitlab.MaintainerPermissions
			})
		}
	}

	mrNumber := mustMR(id).Number

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		notesOptions := gitlab.ListMergeRequestNotesOptions{
			Sort: gitlab.Ptr("asc"),
			ListOptions: gitlab.ListOptions{
				PerPage: int64(_listChangeCommentsPageSize),
			},
		}

		for pageNum := 1; true; pageNum++ {
			notes, response, err := r.client.Notes.ListMergeRequestNotes(
				r.repoID, mrNumber, &notesOptions,
				gitlab.WithContext(ctx),
			)
			if err != nil {
				yield(nil, fmt.Errorf("list comments (page %d): %w", pageNum, err))
				return
			}

			for _, note := range notes {
				match := true
				for _, filter := range filters {
					if !filter(*note) {
						match = false
						break
					}
				}
				if !match {
					continue
				}

				item := &forge.ListChangeCommentItem{
					ID: &MRComment{
						Number:   note.ID,
						MRNumber: mrNumber,
					},
					Body: note.Body,
				}

				if !yield(item, nil) {
					return
				}
			}

			if response.CurrentPage >= response.TotalPages {
				return
			}

			notesOptions.Page = response.NextPage
		}
	}
}
