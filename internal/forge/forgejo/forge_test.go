package forgejo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestURLs(t *testing.T) {
	tests := []struct {
		name string
		opts Options

		wantURL    string
		wantAPIURL string
	}{
		{
			name:       "Default",
			wantURL:    DefaultURL,
			wantAPIURL: DefaultURL,
		},
		{
			name:       "CustomURL",
			opts:       Options{URL: "https://forgejo.example.com"},
			wantURL:    "https://forgejo.example.com",
			wantAPIURL: "https://forgejo.example.com",
		},
		{
			name: "CustomBoth",
			opts: Options{
				URL:    "https://forgejo.example.com",
				APIURL: "https://forgejo.example.com/api/v1",
			},
			wantURL:    "https://forgejo.example.com",
			wantAPIURL: "https://forgejo.example.com/api/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: tt.opts}

			assert.Equal(t, tt.wantURL, f.URL())
			assert.Equal(t, tt.wantAPIURL, f.APIURL())
		})
	}
}

func TestParseRepositoryPath(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{
			name: "HTTPS",
			give: "/example/repo",
			want: "example/repo",
		},
		{
			name: "GitSuffix",
			give: "/example/repo.git",
			want: "example/repo",
		},
		{
			name: "TrailingSlash",
			give: "/example/repo/",
			want: "example/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := new(Forge).ParseRepositoryPath(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, id.String())
		})
	}
}

func TestParseRepositoryPath_error(t *testing.T) {
	_, err := new(Forge).ParseRepositoryPath("/repo-only")
	require.ErrorIs(t, err, forge.ErrUnsupportedURL)
}

func TestRepositoryID_ChangeURL(t *testing.T) {
	rid := &RepositoryID{
		url:   "https://codeberg.org",
		owner: "example",
		name:  "repo",
	}

	assert.Equal(
		t,
		"https://codeberg.org/example/repo/pulls/42",
		rid.ChangeURL(&PR{Number: 42}),
	)
}

func TestChangeIDJSON(t *testing.T) {
	data, err := new(Forge).MarshalChangeID(&PR{Number: 42})
	require.NoError(t, err)
	assert.JSONEq(t, `{"number":42}`, string(data))

	id, err := new(Forge).UnmarshalChangeID(data)
	require.NoError(t, err)
	assert.Equal(t, "#42", id.String())
}

func TestChangeMetadataJSON(t *testing.T) {
	md := &PRMetadata{
		PR: &PR{Number: 42},
		NavigationComment: &PRComment{
			ID:       100,
			PRNumber: 42,
		},
	}

	data, err := new(Forge).MarshalChangeMetadata(md)
	require.NoError(t, err)

	got, err := new(Forge).UnmarshalChangeMetadata(data)
	require.NoError(t, err)
	assert.Equal(t, "#42", got.ChangeID().String())
	assert.Equal(t, "100", got.NavigationCommentID().String())
}
