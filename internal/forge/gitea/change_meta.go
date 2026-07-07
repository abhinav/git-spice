package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// PRMetadata is the metadata for a pull request
// persisted in git-spice's data store.
type PRMetadata struct {
	// PR is the pull request this metadata is for.
	PR *PR `json:"pr,omitempty"`

	// NavigationComment is the comment on the pull request
	// where we visualize the stack.
	NavigationComment *PRComment `json:"comment,omitempty"`
}

var _ forge.ChangeMetadata = (*PRMetadata)(nil)

// ForgeID reports the forge ID that owns this metadata.
func (*PRMetadata) ForgeID() string {
	return "gitea"
}

// ChangeID reports the change ID of the pull request.
func (m *PRMetadata) ChangeID() forge.ChangeID {
	return m.PR
}

// NavigationCommentID reports the comment ID of the navigation comment.
func (m *PRMetadata) NavigationCommentID() forge.ChangeCommentID {
	if m.NavigationComment == nil {
		return nil
	}
	return m.NavigationComment
}

// SetNavigationCommentID sets the ID of the navigation comment.
func (m *PRMetadata) SetNavigationCommentID(id forge.ChangeCommentID) {
	m.NavigationComment = mustPRComment(id)
}

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(_ context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
	return &PRMetadata{PR: mustPR(id)}, nil
}

type changeMetadataCodec struct{}

// MarshalChangeMetadata serializes a PRMetadata into JSON.
func (changeMetadataCodec) MarshalChangeMetadata(md forge.ChangeMetadata) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata deserializes a PRMetadata from JSON.
func (changeMetadataCodec) UnmarshalChangeMetadata(data json.RawMessage) (forge.ChangeMetadata, error) {
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

// PR uniquely identifies a pull request in Gitea.
// It implements [forge.ChangeID].
type PR struct {
	// Number is the pull request number.
	Number int64 `json:"number"` // required
}

var _ forge.ChangeID = (*PR)(nil)

func mustPR(id forge.ChangeID) *PR {
	pr, ok := id.(*PR)
	if !ok {
		panic(fmt.Sprintf("gitea: expected *PR, got %T", id))
	}
	return pr
}

func (id *PR) String() string {
	return fmt.Sprintf("#%d", id.Number)
}

// PRComment identifies a comment on a Gitea pull request.
// It implements [forge.ChangeCommentID].
type PRComment struct {
	// ID is the comment ID.
	ID int64 `json:"id"` // required
}

var _ forge.ChangeCommentID = (*PRComment)(nil)

func mustPRComment(id forge.ChangeCommentID) *PRComment {
	if id == nil {
		return nil
	}
	c, ok := id.(*PRComment)
	if !ok {
		panic(fmt.Sprintf("gitea: unexpected comment type: %T", id))
	}
	return c
}

func (c *PRComment) String() string {
	return strconv.FormatInt(c.ID, 10)
}
