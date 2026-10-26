package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPRMetadataRoundTrip(t *testing.T) {
	var f Forge

	orig := &PRMetadata{
		PR: &PR{Number: 42},
		NavigationComment: &PRComment{
			ID:      101,
			PRID:    42,
			Version: 7,
		},
	}

	data, err := f.MarshalChangeMetadata(orig)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeMetadata(data)
	require.NoError(t, err)

	md, ok := got.(*PRMetadata)
	require.True(t, ok, "expected *PRMetadata, got %T", got)

	require.NotNil(t, md.PR)
	assert.Equal(t, int64(42), md.PR.Number)

	require.NotNil(t, md.NavigationComment)
	assert.Equal(t, int64(101), md.NavigationComment.ID)
	assert.Equal(t, int64(42), md.NavigationComment.PRID)
	assert.Equal(t, 7, md.NavigationComment.Version)

	assert.Equal(t, orig, md)
}

func TestPRMetadataRoundTrip_noComment(t *testing.T) {
	var f Forge

	orig := &PRMetadata{PR: &PR{Number: 7}}

	data, err := f.MarshalChangeMetadata(orig)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeMetadata(data)
	require.NoError(t, err)

	md, ok := got.(*PRMetadata)
	require.True(t, ok, "expected *PRMetadata, got %T", got)

	require.NotNil(t, md.PR)
	assert.Equal(t, int64(7), md.PR.Number)
	assert.Nil(t, md.NavigationComment)
	assert.Equal(t, orig, md)
}

func TestChangeIDRoundTrip(t *testing.T) {
	var f Forge

	orig := &PR{Number: 123}

	data, err := f.MarshalChangeID(orig)
	require.NoError(t, err)

	got, err := f.UnmarshalChangeID(data)
	require.NoError(t, err)

	pr, ok := got.(*PR)
	require.True(t, ok, "expected *PR, got %T", got)
	assert.Equal(t, int64(123), pr.Number)
	assert.Equal(t, orig, pr)
}

func TestUnmarshalChangeMetadata_invalid(t *testing.T) {
	var f Forge

	_, err := f.UnmarshalChangeMetadata([]byte("not json"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "unmarshal PR metadata")
}

func TestUnmarshalChangeID_invalid(t *testing.T) {
	var f Forge

	_, err := f.UnmarshalChangeID([]byte("not json"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "unmarshal PR ID")
}

func TestPRMetadata_releasedCloudJSON(t *testing.T) {
	var f Forge

	const releasedJSON = `{"pr":{"number":42},"comment":{"id":7,"pr_id":42}}`

	t.Run("Marshal", func(t *testing.T) {
		data, err := f.MarshalChangeMetadata(&PRMetadata{
			PR:                &PR{Number: 42},
			NavigationComment: &PRComment{ID: 7, PRID: 42},
		})
		require.NoError(t, err)

		assert.Equal(t, releasedJSON, string(data))
	})

	t.Run("Unmarshal", func(t *testing.T) {
		got, err := f.UnmarshalChangeMetadata([]byte(releasedJSON))
		require.NoError(t, err)

		md, ok := got.(*PRMetadata)
		require.True(t, ok, "expected *PRMetadata, got %T", got)
		assert.Equal(t, &PRMetadata{
			PR:                &PR{Number: 42},
			NavigationComment: &PRComment{ID: 7, PRID: 42},
		}, md)
	})
}

func TestPRMetadata_commentVersionJSON(t *testing.T) {
	var f Forge

	data, err := f.MarshalChangeMetadata(&PRMetadata{
		PR:                &PR{Number: 42},
		NavigationComment: &PRComment{ID: 7, PRID: 42, Version: 3},
	})
	require.NoError(t, err)

	assert.Equal(t,
		`{"pr":{"number":42},"comment":{"id":7,"pr_id":42,"version":3}}`,
		string(data))

	got, err := f.UnmarshalChangeMetadata(data)
	require.NoError(t, err)

	md, ok := got.(*PRMetadata)
	require.True(t, ok, "expected *PRMetadata, got %T", got)
	require.NotNil(t, md.NavigationComment)
	assert.Equal(t, 3, md.NavigationComment.Version)
}
