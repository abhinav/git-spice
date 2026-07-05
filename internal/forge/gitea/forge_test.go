package gitea

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestForge_ID(t *testing.T) {
	assert.Equal(t, "gitea", new(Forge).ID())
}

func TestForge_URLs(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		wantURL    string
		wantAPIURL string
	}{
		{
			name:       "BothEmpty",
			wantURL:    "",
			wantAPIURL: "",
		},
		{
			name:       "URLOnly",
			opts:       Options{URL: "https://gitea.example.com"},
			wantURL:    "https://gitea.example.com",
			wantAPIURL: "https://gitea.example.com",
		},
		{
			name:       "CustomAPIURL",
			opts:       Options{URL: "https://gitea.example.com", APIURL: "https://api.gitea.example.com"},
			wantURL:    "https://gitea.example.com",
			wantAPIURL: "https://api.gitea.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteURL, err := giturl.Parse("https://gitea.example.com/owner/repo")
			require.NoError(t, err)

			f, err := (&Definition{Options: tt.opts}).New(remoteURL)
			require.NoError(t, err)

			assert.Equal(t, tt.wantURL, f.BaseURL())
			assert.Equal(t, tt.wantAPIURL, f.(*Forge).apiURL())
		})
	}
}

func TestForge_ParseRepositoryPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "ValidPath",
			path:      "/scotty/warp-core.git",
			wantOwner: "scotty",
			wantRepo:  "warp-core",
		},
		{
			name:      "ValidPathNoGit",
			path:      "/scotty/warp-core",
			wantOwner: "scotty",
			wantRepo:  "warp-core",
		},
		{
			name:    "MissingRepo",
			path:    "/scotty",
			wantErr: true,
		},
		{
			name:    "Empty",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{Options: Options{URL: "https://gitea.example.com"}}
			rid, err := f.ParseRepositoryPath(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOwner+"/"+tt.wantRepo, rid.String())
		})
	}
}

func TestRepositoryID_ChangeURL(t *testing.T) {
	rid := &RepositoryID{
		url:   "https://gitea.example.com",
		owner: "scotty",
		name:  "warp-core",
	}
	assert.Equal(t,
		"https://gitea.example.com/scotty/warp-core/pulls/42",
		rid.ChangeURL(&PR{Number: 42}),
	)
}

func TestDefinition_CLIPlugin(t *testing.T) {
	d := new(Definition)
	assert.Equal(t, &d.Options, d.CLIPlugin())
}
