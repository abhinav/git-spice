package shamhub

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMetadata records the metadata for a change on a ShamHub server.
type ChangeMetadata struct {
	Number int `json:"number"`

	NavigationComment int `json:"nav_comment"`
}

// ForgeID reports the forge ID that owns this metadata.
func (*ChangeMetadata) ForgeID() string {
	return "shamhub"
}

// ChangeID reports the change ID of the change.
func (m *ChangeMetadata) ChangeID() forge.ChangeID {
	return ChangeID(m.Number)
}

// NavigationCommentID reports the comment ID of the navigation comment.
func (m *ChangeMetadata) NavigationCommentID() forge.ChangeCommentID {
	if m.NavigationComment == 0 {
		return nil
	}
	return ChangeCommentID(m.NavigationComment)
}

// SetNavigationCommentID sets the comment ID of the navigation comment.
// id may be nil.
func (m *ChangeMetadata) SetNavigationCommentID(id forge.ChangeCommentID) {
	if id == nil {
		m.NavigationComment = 0
	} else {
		m.NavigationComment = int(id.(ChangeCommentID))
	}
}

// NewChangeMetadata returns the metadata for a change on a ShamHub server.
func (f *forgeRepository) NewChangeMetadata(_ context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
	return &ChangeMetadata{
		Number: int(id.(ChangeID)),
	}, nil
}

// MarshalChangeMetadata marshals the given change metadata to JSON.
func (f *Forge) MarshalChangeMetadata(md forge.ChangeMetadata) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata unmarshals the given JSON data to change metadata.
func (f *Forge) UnmarshalChangeMetadata(data json.RawMessage) (forge.ChangeMetadata, error) {
	var md ChangeMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal change metadata: %w", err)
	}
	return &md, nil
}

// MarshalChangeID marshals the given change ID to JSON.
func (f *Forge) MarshalChangeID(id forge.ChangeID) (json.RawMessage, error) {
	return json.Marshal(id.(ChangeID))
}

// UnmarshalChangeID unmarshals the given JSON data to change ID.
func (f *Forge) UnmarshalChangeID(data json.RawMessage) (forge.ChangeID, error) {
	var id ChangeID
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("unmarshal change ID: %w", err)
	}
	return id, nil
}
