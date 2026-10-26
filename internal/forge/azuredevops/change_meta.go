package azuredevops

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// PRMetadata is the metadata for a pull request.
type PRMetadata struct {
	PR *PR `json:"pr,omitzero"`

	NavigationComment *PRComment `json:"comment,omitzero"`
}

var _ forge.ChangeMetadata = (*PRMetadata)(nil)

// ForgeID reports the forge ID that owns this metadata.
func (*PRMetadata) ForgeID() string {
	return "azuredevops"
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
func (changeMetadataCodec) MarshalChangeMetadata(md forge.ChangeMetadata) (jsontext.Value, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata deserializes a PRMetadata from JSON.
func (changeMetadataCodec) UnmarshalChangeMetadata(data jsontext.Value) (forge.ChangeMetadata, error) {
	var md PRMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal PR metadata: %w", err)
	}
	return &md, nil
}

// MarshalChangeID serializes a PR into JSON.
func (*Forge) MarshalChangeID(cid forge.ChangeID) (jsontext.Value, error) {
	return json.Marshal(mustPR(cid))
}

// UnmarshalChangeID deserializes a PR from JSON.
func (*Forge) UnmarshalChangeID(data jsontext.Value) (forge.ChangeID, error) {
	var pr PR
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("unmarshal PR: %w", err)
	}
	return &pr, nil
}

// PR uniquely identifies a PR in an Azure DevOps repository.
// It's a valid forge.ChangeID.
type PR struct {
	// Number is the pull request ID.
	Number int `json:"number"`
}

var _ forge.ChangeID = (*PR)(nil)

func mustPR(cid forge.ChangeID) *PR {
	pr, ok := cid.(*PR)
	if !ok {
		panic(fmt.Sprintf("unexpected change ID type: %T", cid))
	}
	return pr
}

func (id *PR) String() string {
	return fmt.Sprintf("!%d", id.Number)
}

// PRComment uniquely identifies a comment on a PR.
// Azure DevOps uses a thread-based comment system,
// so we store the PR ID, thread ID, and comment ID.
type PRComment struct {
	// PRID is the ID of the pull request.
	PRID int `json:"prId"`

	// ThreadID is the ID of the comment thread.
	ThreadID int `json:"threadId"`

	// CommentID is the ID of the comment within the thread.
	CommentID int `json:"commentId"`
}

var _ forge.ChangeCommentID = (*PRComment)(nil)

func mustPRComment(cid forge.ChangeCommentID) *PRComment {
	if cid == nil {
		return nil
	}
	c, ok := cid.(*PRComment)
	if !ok {
		panic(fmt.Sprintf("unexpected comment ID type: %T", cid))
	}
	return c
}

func (id *PRComment) String() string {
	return fmt.Sprintf("pr-%d-thread-%d-comment-%d", id.PRID, id.ThreadID, id.CommentID)
}

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	_ context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	pr := mustPR(id)
	return &PRMetadata{PR: pr}, nil
}
