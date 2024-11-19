package gitlab

import (
	"context"
	"fmt"
	"iter"
	"strconv"

	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/forge"
)

// MRComment is a ChangeCommentID for a GitLab MR comment.
type MRComment struct {
	Number int `json:"number"`
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
	return strconv.Itoa(c.Number) // TODO: return URL?
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
	_ context.Context,
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

		// Only the author or maintainers can update comments.
		if options.CanUpdate {
			filters = append(filters, func(note gitlab.Note) bool {
				return note.Author.ID == r.userID || *r.userRole >= gitlab.MaintainerPermissions
			})
		}
	}

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		notesOptions := gitlab.ListMergeRequestNotesOptions{
			Sort: gitlab.Ptr("asc"),
			ListOptions: gitlab.ListOptions{
				PerPage: _listChangeCommentsPageSize,
			},
		}

		for pageNum := 1; true; pageNum++ {
			notesOptions.ListOptions.Page = pageNum
			notes, response, err := r.client.Notes.ListMergeRequestNotes(r.repoID, mustMR(id).Number, &notesOptions)
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
						Number: note.ID,
					},
					Body: note.Body,
				}

				if !yield(item, nil) {
					return
				}
			}

			if pageNum >= response.TotalPages {
				return
			}
		}
	}
}
