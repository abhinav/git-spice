package shamhub

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestNewRepository_stacksCapability(t *testing.T) {
	tests := []struct {
		name string
		mode stacksMode
		want bool
	}{
		{name: "Default"},
		{name: "Off", mode: stacksOff},
		{name: "On", mode: stacksOn, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := newRepository(
				&Forge{
					URL:    "https://shamhub.example",
					APIURL: "https://api.shamhub.example",
					Stacks: tt.mode,
				},
				&AuthenticationToken{tok: "test"},
				&RepositoryID{
					url:   "https://shamhub.example/acme/repo.git",
					owner: "acme",
					repo:  "repo",
				},
				http.DefaultClient,
			)
			require.NoError(t, err)

			_, got := repo.(forge.StackRepository)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStacksMode_UnmarshalText(t *testing.T) {
	tests := []struct {
		name string
		give string
		want stacksMode
	}{
		{name: "Off", give: "off", want: stacksOff},
		{name: "Zero", give: "0", want: stacksOff},
		{name: "On", give: "on", want: stacksOn},
		{name: "One", give: "1", want: stacksOn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got stacksMode
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
		})
	}

	var got stacksMode
	assert.Error(t, got.UnmarshalText([]byte("invalid")))
}

func TestForge_ParseRepositoryPath_knownForge(t *testing.T) {
	f := &Forge{URL: "https://shamhub.example"}
	remoteURL, err := giturl.Parse("git@shamhub-alias:example/repo.git")
	require.NoError(t, err)

	rid, err := f.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err)

	assert.Equal(t, "example/repo", rid.String())
	assert.Equal(t,
		"https://shamhub.example/example/repo/change/123",
		rid.ChangeURL(ChangeID(123)))
}
