package forgejo

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// PR uniquely identifies a Pull Request in Forgejo.
// It is a valid [forge.ChangeID].
type PR struct {
	// Number is the repository-local pull request number.
	Number int64 `json:"number"` // required
}

var _ forge.ChangeID = (*PR)(nil)

func mustPR(id forge.ChangeID) *PR {
	pr, ok := id.(*PR)
	if !ok {
		panic(fmt.Sprintf("forgejo: expected *PR, got %T", id))
	}
	return pr
}

func (id *PR) String() string {
	return fmt.Sprintf("#%d", id.Number)
}

// PRMetadata is the metadata for a pull request
// persisted in git-spice's data store.
type PRMetadata struct {
	// PR is the pull request this metadata is for.
	PR *PR `json:"pr,omitempty"`

	// NavigationComment is the comment on the pull request
	// where we visualize the stack of PRs.
	NavigationComment *PRComment `json:"comment,omitempty"`
}

var _ forge.ChangeMetadata = (*PRMetadata)(nil)

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	_ context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	return &PRMetadata{PR: mustPR(id)}, nil
}

// ForgeID reports the forge ID that owns this metadata.
func (*PRMetadata) ForgeID() string {
	return "forgejo"
}

// ChangeID reports the change ID of the pull request.
func (m *PRMetadata) ChangeID() forge.ChangeID {
	return m.PR
}

// NavigationCommentID reports the comment ID of the navigation comment
// left on the pull request.
func (m *PRMetadata) NavigationCommentID() forge.ChangeCommentID {
	if m.NavigationComment == nil {
		return nil
	}
	return m.NavigationComment
}

// SetNavigationCommentID sets the comment ID of the navigation comment
// left on the pull request.
//
// id may be nil.
func (m *PRMetadata) SetNavigationCommentID(id forge.ChangeCommentID) {
	m.NavigationComment = mustPRComment(id)
}

type changeMetadataCodec struct{}

// MarshalChangeMetadata serializes a PRMetadata into JSON.
func (changeMetadataCodec) MarshalChangeMetadata(
	md forge.ChangeMetadata,
) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata deserializes a PRMetadata from JSON.
func (changeMetadataCodec) UnmarshalChangeMetadata(
	data json.RawMessage,
) (forge.ChangeMetadata, error) {
	var md PRMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal PR metadata: %w", err)
	}
	return &md, nil
}

// MarshalChangeID serializes a PR into JSON.
func (*Forge) MarshalChangeID(id forge.ChangeID) (json.RawMessage, error) {
	return json.Marshal(mustPR(id))
}

// UnmarshalChangeID deserializes a PR from JSON.
func (*Forge) UnmarshalChangeID(data json.RawMessage) (forge.ChangeID, error) {
	var id PR
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("unmarshal PR ID: %w", err)
	}
	return &id, nil
}
