package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// MRMetadata is the metadata for a merge request.
type MRMetadata struct {
	MR *MR `json:"mr,omitempty"`

	NavigationComment *MRComment `json:"comment,omitempty"`
}

var _ forge.ChangeMetadata = (*MRMetadata)(nil)

// ForgeID reports the forge ID that owns this metadata.
func (*MRMetadata) ForgeID() string {
	return "gitlab"
}

// ChangeID reports the change ID of the pull request.
func (m *MRMetadata) ChangeID() forge.ChangeID {
	return m.MR
}

// NavigationCommentID reports the comment ID of the navigation comment
// left on the merge request.
func (m *MRMetadata) NavigationCommentID() forge.ChangeCommentID {
	if m.NavigationComment == nil {
		return nil
	}
	return m.NavigationComment
}

// SetNavigationCommentID sets the comment ID of the navigation comment
// left on the merge request.
//
// id may be nil.
func (m *MRMetadata) SetNavigationCommentID(id forge.ChangeCommentID) {
	m.NavigationComment = mustMRComment(id)
}

// NewChangeMetadata returns the metadata for a merge request.
func (r *Repository) NewChangeMetadata(_ context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
	mr := mustMR(id)
	return &MRMetadata{MR: mr}, nil
}

// MR uniquely identifies an MR in GitLab repository.
// It's a valid forge.ChangeID.
type MR struct {
	// Number is the merge request number.
	// This will always be set.
	Number int `json:"number"`
}

var _ forge.ChangeID = (*MR)(nil)

func mustMR(id forge.ChangeID) *MR {
	mr, ok := id.(*MR)
	if !ok {
		panic(fmt.Sprintf("gitlab: expected *MR, got %T", id))
	}
	return mr
}

func (id *MR) String() string {
	return fmt.Sprintf("!%d", id.Number)
}
