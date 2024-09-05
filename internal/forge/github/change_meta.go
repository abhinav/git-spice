package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
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
func (f *Repository) NewChangeMetadata(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMetadata, error) {
	pr := mustPR(id)

	var err error
	pr.GQLID, err = f.graphQLID(ctx, pr) // ensure GQL ID is set
	if err != nil {
		return nil, fmt.Errorf("get pull request ID: %w", err)
	}

	return &PRMetadata{PR: pr}, nil
}

// MarshalChangeMetadata serializes a PRMetadata into JSON.
func (*Forge) MarshalChangeMetadata(md forge.ChangeMetadata) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata deserializes a PRMetadata from JSON.
func (*Forge) UnmarshalChangeMetadata(data json.RawMessage) (forge.ChangeMetadata, error) {
	var md PRMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal PR metadata: %w", err)
	}
	return &md, nil
}

// PR uniquely identifies a PR in a GitHub repository.
// It's a valid forge.ChangeID.
type PR struct {
	// Number is the pull request number.
	// This will always be set.
	Number int `json:"number"`

	// GQLID is the GraphQL ID of the change.
	// This may be nil or empty.
	GQLID githubv4.ID `json:"gqlID,omitempty"`
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
func (f *Repository) graphQLID(ctx context.Context, gid *PR) (githubv4.ID, error) {
	if gid.GQLID != "" && gid.GQLID != nil {
		return gid.GQLID, nil
	}

	var q struct {
		Repository struct {
			PullRequest struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"pullRequest(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	if err := f.client.Query(ctx, &q, map[string]any{
		"owner":  githubv4.String(f.owner),
		"repo":   githubv4.String(f.repo),
		"number": githubv4.Int(gid.Number),
	}); err != nil {
		return nil, fmt.Errorf("get pull request ID: %w", err)
	}

	gid.GQLID = q.Repository.PullRequest.ID
	return gid.GQLID, nil
}
