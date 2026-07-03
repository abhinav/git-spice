package gitea

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestPR_String(t *testing.T) {
	assert.Equal(t, "#42", (&PR{Number: 42}).String())
}

func TestPRComment_String(t *testing.T) {
	assert.Equal(t, "88", (&PRComment{ID: 88}).String())
}

func TestForge_MarshalUnmarshalChangeID(t *testing.T) {
	f := new(Forge)
	original := &PR{Number: 42}

	data, err := f.MarshalChangeID(original)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeID(data)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestForge_MarshalUnmarshalChangeMetadata(t *testing.T) {
	f := new(Forge)
	original := &PRMetadata{
		PR:                &PR{Number: 42},
		NavigationComment: &PRComment{ID: 88},
	}

	data, err := f.MarshalChangeMetadata(original)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeMetadata(data)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestForge_MarshalChangeMetadata_noComment(t *testing.T) {
	f := new(Forge)
	original := &PRMetadata{PR: &PR{Number: 7}}

	data, err := f.MarshalChangeMetadata(original)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeMetadata(data)
	require.NoError(t, err)

	md := got.(*PRMetadata)
	assert.Nil(t, md.NavigationComment)
	assert.Equal(t, int64(7), md.PR.Number)
}

func TestPRMetadata_ForgeID(t *testing.T) {
	assert.Equal(t, "gitea", (&PRMetadata{}).ForgeID())
}

func TestPRMetadata_NavigationCommentID(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		md := &PRMetadata{PR: &PR{Number: 1}}
		assert.Nil(t, md.NavigationCommentID())
	})

	t.Run("set", func(t *testing.T) {
		md := &PRMetadata{
			PR:                &PR{Number: 1},
			NavigationComment: &PRComment{ID: 5},
		}
		got := md.NavigationCommentID()
		require.NotNil(t, got)
		assert.Equal(t, "5", got.String())
	})
}

func TestPRMetadata_SetNavigationCommentID(t *testing.T) {
	md := &PRMetadata{PR: &PR{Number: 1}}
	md.SetNavigationCommentID(&PRComment{ID: 99})

	assert.Equal(t, int64(99), md.NavigationComment.ID)
}

func TestPRMetadata_SetNavigationCommentID_nil(t *testing.T) {
	md := &PRMetadata{
		PR:                &PR{Number: 1},
		NavigationComment: &PRComment{ID: 5},
	}
	md.SetNavigationCommentID(nil)
	assert.Nil(t, md.NavigationComment)
}

func TestForge_UnmarshalChangeID_invalid(t *testing.T) {
	f := new(Forge)
	_, err := f.UnmarshalChangeID(json.RawMessage(`not-json`))
	require.Error(t, err)
}

func TestForge_UnmarshalChangeMetadata_invalid(t *testing.T) {
	f := new(Forge)
	_, err := f.UnmarshalChangeMetadata(json.RawMessage(`not-json`))
	require.Error(t, err)
}

// Verify interface implementations at compile time.
var (
	_ forge.ChangeID        = (*PR)(nil)
	_ forge.ChangeCommentID = (*PRComment)(nil)
	_ forge.ChangeMetadata  = (*PRMetadata)(nil)
)
