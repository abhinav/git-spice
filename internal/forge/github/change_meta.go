package github

import (
	"context"
	"encoding/json"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// PRMetadata is the metadata for a pull request.
type PRMetadata struct {
	PR *PR `json:"pr,omitempty"`

	NavigationComment *PRComment `json:"comment,omitempty"`
}

var _ forge.ChangeMetadata = (*PRMetadata)(nil)

// ForgeID reports the forge ID that owns this metadata.
func (*PRMetadata) ForgeID() string {
	return "github"
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

// NewChangeMetadata returns the metadata for a pull request.
func (r *Repository) NewChangeMetadata(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	pr := mustPR(id)

	var err error
	pr.GQLID, err = r.graphQLID(ctx, pr) // ensure GQL ID is set
	if err != nil {
		return nil, fmt.Errorf("get pull request ID: %w", err)
	}

	return &PRMetadata{PR: pr}, nil
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
func (*Forge) MarshalChangeID(cid forge.ChangeID) (json.RawMessage, error) {
	return json.Marshal(mustPR(cid))
}

// UnmarshalChangeID deserializes a PR from JSON.
func (*Forge) UnmarshalChangeID(data json.RawMessage) (forge.ChangeID, error) {
	var pr PR
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("unmarshal PR: %w", err)
	}
	return &pr, nil
}

// PR uniquely identifies a PR in a GitHub repository.
// It's a valid forge.ChangeID.
type PR struct {
	// Number is the pull request number.
	// This will always be set.
	Number int `json:"number"`

	// GQLID is the GraphQL ID of the change.
	// This may be empty.
	GQLID github.ID `json:"gqlID,omitempty"`
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
	return fmt.Sprintf("#%d", id.Number)
}

// UnmarshalJSON unmarshals a GitHub change ID.
// It accepts the following formats:
//
//	{"number": 123, "gqlID": "..."}
//	123
//
// The second format is for backwards compatibility.
func (id *PR) UnmarshalJSON(data []byte) error {
	if num := 0; json.Unmarshal(data, &num) == nil && num > 0 {
		id.Number = num
		return nil
	}

	type newFormat PR
	if err := json.Unmarshal(data, (*newFormat)(id)); err != nil {
		return fmt.Errorf("unmarshal GitHub change ID: %w", err)
	}

	return nil
}

// graphQLID returns the GraphQL ID of the change.
// It will retrieve the ID from the GitHub API if it is not already set.
func (r *Repository) graphQLID(ctx context.Context, gid *PR) (github.ID, error) {
	if gid.GQLID != "" {
		return gid.GQLID, nil
	}

	id, err := r.gateway.PullRequestID(ctx, r.owner, r.repo, gid.Number)
	if err != nil {
		return "", fmt.Errorf("get pull request ID: %w", err)
	}

	gid.GQLID = id
	return gid.GQLID, nil
}
