package gitlab

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// MRMetadata is the metadata for a merge request
// persisted in git-spice's data store.
type MRMetadata struct {
	// MR is the merge request this metadata is for.
	MR *MR `json:"mr,omitempty"`

	// NavigationComment is the comment on the merge request
	// where we visualize the stack of MRs.
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

// MarshalChangeMetadata serializes a MRMetadata into JSON.
func (*Forge) MarshalChangeMetadata(md forge.ChangeMetadata) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata deserializes a MRMetadata from JSON.
func (*Forge) UnmarshalChangeMetadata(data json.RawMessage) (forge.ChangeMetadata, error) {
	var md MRMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal MR metadata: %w", err)
	}
	return &md, nil
}

// MarshalChangeID serializes a MR into JSON.
func (*Forge) MarshalChangeID(id forge.ChangeID) (json.RawMessage, error) {
	return json.Marshal(mustMR(id))
}

// UnmarshalChangeID deserializes a MR from JSON.
func (*Forge) UnmarshalChangeID(data json.RawMessage) (forge.ChangeID, error) {
	var id MR
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("unmarshal MR ID: %w", err)
	}
	return &id, nil
}

// MR uniquely identifies a Merge Request in GitLab.
// It's a valid forge.ChangeID.
type MR struct {
	// Number is the merge request number.
	// This will always be set.
	Number int64 `json:"number"` // required
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

// UnmarshalJSON unmarshals a GitLab change ID.
// It accepts the following format: {"number": 123}
func (id *MR) UnmarshalJSON(data []byte) error {
	type newFormat MR
	if err := json.Unmarshal(data, (*newFormat)(id)); err != nil {
		return fmt.Errorf("unmarshal GitLab change ID: %w", err)
	}

	return nil
}
