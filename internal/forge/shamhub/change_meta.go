package shamhub

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMetadata records the metadata for a change on a ShamHub server.
type ChangeMetadata struct {
	Number       int `json:"number"`
	StackComment int `json:"stack_comment"`
}

// ForgeID reports the forge ID that owns this metadata.
func (*ChangeMetadata) ForgeID() string {
	return "shamhub" // TODO: const
}

// ChangeID reports the change ID of the change.
func (m *ChangeMetadata) ChangeID() forge.ChangeID {
	return ChangeID(m.Number)
}

// StackCommentID reports the comment ID of the stack comment.
func (m *ChangeMetadata) StackCommentID() forge.ChangeCommentID {
	if m.StackComment == 0 {
		return nil
	}
	return ChangeCommentID(m.StackComment)
}

// SetStackCommentID sets the comment ID of the stack comment.
// id may be nil.
func (m *ChangeMetadata) SetStackCommentID(id forge.ChangeCommentID) {
	if id == nil {
		m.StackComment = 0
	} else {
		m.StackComment = int(id.(ChangeCommentID))
	}
}

// NewChangeMetadata returns the metadata for a change on a ShamHub server.
func (f *forgeRepository) NewChangeMetadata(ctx context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
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
