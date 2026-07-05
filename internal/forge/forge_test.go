package forge_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/bitbucket"
	"go.abhg.dev/gs/internal/forge/forgejo"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/gitea"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.uber.org/mock/gomock"
)

func TestRegister(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockForge(ctrl)
	mockForge.EXPECT().ID().Return("a").AnyTimes()
	mockForge.EXPECT().BaseURL().Return("https://example.com").AnyTimes()

	mockHandle := forgetest.NewMockRepositoryID(ctrl)
	mockForge.EXPECT().ParseRepositoryPath(gomock.Any()).
		DoAndReturn(func(path string) (forge.RepositoryID, error) {
			if path == "/foo" {
				return mockHandle, nil
			}

			return nil, fmt.Errorf("%w: unexpected path %q",
				forge.ErrUnsupportedURL, path)
		}).AnyTimes()

	var registry forge.Registry
	defer registry.Register(testDefinition{
		id: "a",
		new: func(*giturl.URL) (forge.Forge, error) {
			return mockForge, nil
		},
	})()

	t.Run("All", func(t *testing.T) {
		var ok bool
		for f := range registry.All() {
			if f.ID() == "a" {
				ok = true
				break
			}
		}
		assert.True(t, ok, "forge not found")
	})

	t.Run("Lookup", func(t *testing.T) {
		f, ok := registry.Lookup("a")
		assert.True(t, ok, "forge not found")
		assert.Equal(t, "a", f.ID(), "forge ID mismatch")

		t.Run("NotFound", func(t *testing.T) {
			_, ok := registry.Lookup("b")
			assert.False(t, ok, "unexpected forge match")
		})
	})
}

func TestRegistry_New(t *testing.T) {
	ctrl := gomock.NewController(t)

	remoteURL, err := giturl.Parse("https://example.com/foo")
	require.NoError(t, err)

	var gotRemoteURL *giturl.URL
	var registry forge.Registry
	defer registry.Register(testDefinition{
		id: "a",
		new: func(remoteURL *giturl.URL) (forge.Forge, error) {
			gotRemoteURL = remoteURL

			mockForge := forgetest.NewMockForge(ctrl)
			mockForge.EXPECT().ID().Return("a").AnyTimes()
			return mockForge, nil
		},
	})()

	first, err := registry.New("a", remoteURL)
	require.NoError(t, err)
	second, err := registry.New("a", remoteURL)
	require.NoError(t, err)

	assert.Same(t, remoteURL, gotRemoteURL)
	assert.NotSame(t, first, second)
}

func TestRegistry_New_requiresRemoteURL(t *testing.T) {
	var registry forge.Registry
	defer registry.Register(testDefinition{
		id:      "a",
		baseURL: "https://example.com",
		new: func(*giturl.URL) (forge.Forge, error) {
			t.Fatal("New should reject before calling the definition")
			return nil, nil
		},
	})()

	_, err := registry.New("a", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
}

func TestInferFromRemoteURL_codebergForgejo(t *testing.T) {
	var registry forge.Registry
	defer registry.Register(new(forgejo.Definition))()

	remoteURL, err := giturl.Parse("git@codeberg.org:example/repo.git")
	require.NoError(t, err)

	f, rid, ok := forge.InferFromRemoteURL(&registry, remoteURL)
	require.True(t, ok, "forge not found")
	assert.Equal(t, "forgejo", f.ID())
	assert.Equal(t, "example/repo", rid.String())
}

func TestDefinition_New_rejectsMismatchedConfiguredURL(t *testing.T) {
	remoteURL, err := giturl.Parse("https://other.example.com/owner/repo")
	require.NoError(t, err)

	tests := []struct {
		name string
		def  forge.Definition
	}{
		{
			name: "Bitbucket",
			def: &bitbucket.Definition{
				Options: bitbucket.Options{URL: "https://bitbucket.example.com"},
			},
		},
		{
			name: "Forgejo",
			def: &forgejo.Definition{
				Options: forgejo.Options{URL: "https://forgejo.example.com"},
			},
		},
		{
			name: "Gitea",
			def: &gitea.Definition{
				Options: gitea.Options{URL: "https://gitea.example.com"},
			},
		},
		{
			name: "GitHub",
			def: &github.Definition{
				Options: github.Options{URL: "https://github.example.com"},
			},
		},
		{
			name: "GitLab",
			def: &gitlab.Definition{
				Options: gitlab.Options{URL: "https://gitlab.example.com"},
			},
		},
		{
			name: "ShamHub",
			def: &shamhub.Definition{
				Options: shamhub.Options{URL: "https://shamhub.example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.def.New(remoteURL)
			require.Error(t, err)
			assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
		})
	}
}

func TestInferFromRemoteURL(t *testing.T) {
	ctrl := gomock.NewController(t)

	tests := []struct {
		name      string
		baseURL   string
		remoteURL string
	}{
		{
			name:      "MatchingHost",
			baseURL:   "https://example.com",
			remoteURL: "https://example.com/foo",
		},
		{
			name:      "Subdomain",
			baseURL:   "https://example.com",
			remoteURL: "ssh://git@ssh.example.com/foo",
		},
		{
			name:      "RemotePort",
			baseURL:   "https://example.com",
			remoteURL: "ssh://git@example.com:2222/foo",
		},
		{
			name:      "ExplicitBasePort",
			baseURL:   "https://example.com:8443",
			remoteURL: "ssh://git@example.com:8443/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandle := forgetest.NewMockRepositoryID(ctrl)
			mockForge := forgetest.NewMockForge(ctrl)
			mockForge.EXPECT().ID().Return("a").AnyTimes()
			mockForge.EXPECT().
				ParseRepositoryPath("/foo").
				Return(mockHandle, nil)

			var registry forge.Registry
			defer registry.Register(testDefinition{
				id:      "a",
				baseURL: tt.baseURL,
				new: func(*giturl.URL) (forge.Forge, error) {
					return mockForge, nil
				},
			})()

			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			f, h, ok := forge.InferFromRemoteURL(&registry, remoteURL)
			assert.True(t, ok, "forge not found")
			assert.Equal(t, "a", f.ID(), "forge ID mismatch")
			assert.Same(t, mockHandle, h, "repository ID mismatch")
		})
	}
}

func TestInferFromRemoteURL_noMatch(t *testing.T) {
	ctrl := gomock.NewController(t)

	tests := []struct {
		name      string
		baseURL   string
		remoteURL string
	}{
		{
			name:      "WrongHost",
			baseURL:   "https://example.com",
			remoteURL: "https://example.org/foo",
		},
		{
			name:      "AliasHost",
			baseURL:   "https://example.com",
			remoteURL: "git@example-alias:foo",
		},
		{
			name:      "InvalidBaseURL",
			baseURL:   "NOT\tA\nVALID URL",
			remoteURL: "https://example.com/foo",
		},
		{
			name:      "ExplicitBasePortMismatch",
			baseURL:   "https://example.com:8443",
			remoteURL: "ssh://git@example.com:2222/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockForge := forgetest.NewMockForge(ctrl)
			mockForge.EXPECT().ID().Return("a").AnyTimes()

			var registry forge.Registry
			defer registry.Register(testDefinition{
				id:      "a",
				baseURL: tt.baseURL,
				new: func(*giturl.URL) (forge.Forge, error) {
					return mockForge, nil
				},
			})()

			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			_, _, ok := forge.InferFromRemoteURL(&registry, remoteURL)
			assert.False(t, ok, "unexpected forge match")
		})
	}
}

func TestInferFromRemoteURL_unsupportedRepositoryPath(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockForge(ctrl)
	mockForge.EXPECT().ID().Return("a").AnyTimes()
	mockForge.EXPECT().
		ParseRepositoryPath("/foo").
		Return(nil, fmt.Errorf("%w: unexpected path", forge.ErrUnsupportedURL))

	var registry forge.Registry
	defer registry.Register(testDefinition{
		id:      "a",
		baseURL: "https://example.com",
		new: func(*giturl.URL) (forge.Forge, error) {
			return mockForge, nil
		},
	})()

	remoteURL, err := giturl.Parse("https://example.com/foo")
	require.NoError(t, err)

	_, _, ok := forge.InferFromRemoteURL(&registry, remoteURL)
	assert.False(t, ok, "unexpected forge match")
}

func TestGetDisplayName(t *testing.T) {
	ctrl := gomock.NewController(t)

	t.Run("WithoutDisplayName", func(t *testing.T) {
		mockForge := forgetest.NewMockForge(ctrl)
		mockForge.EXPECT().ID().Return("test-forge")

		name := forge.GetDisplayName(mockForge)
		assert.Equal(t, "test-forge", name)
	})
}

func TestSplitRepositoryPath(t *testing.T) {
	tests := []struct {
		name      string
		give      string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "Simple",
			give:      "/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "WithGitSuffix",
			give:      "/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "TrailingSlash",
			give:      "/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "BothSuffixAndSlash",
			give:      "/owner/repo.git/",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "NoLeadingSlash",
			give:      "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := forge.SplitRepositoryPath(tt.give)

			require.True(t, ok)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func TestSplitRepositoryPath_noMatch(t *testing.T) {
	tests := []struct {
		name string
		give string
	}{
		{
			name: "NoRepoComponent",
			give: "/owner",
		},
		{
			name: "Empty",
			give: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := forge.SplitRepositoryPath(tt.give)
			assert.False(t, ok)
		})
	}
}

type testDefinition struct {
	id      string
	baseURL string
	plugin  any
	new     func(*giturl.URL) (forge.Forge, error)
}

func (d testDefinition) ID() string { return d.id }

func (d testDefinition) BaseURL() string { return d.baseURL }

func (d testDefinition) CLIPlugin() any { return d.plugin }

func (testDefinition) MarshalChangeMetadata(forge.ChangeMetadata) (json.RawMessage, error) {
	return nil, nil
}

func (testDefinition) UnmarshalChangeMetadata(json.RawMessage) (forge.ChangeMetadata, error) {
	return nil, nil
}

func (d testDefinition) New(remoteURL *giturl.URL) (forge.Forge, error) {
	return d.new(remoteURL)
}

func TestChangeState(t *testing.T) {
	tests := []struct {
		state forge.ChangeState
		str   string
	}{
		{forge.ChangeOpen, "open"},
		{forge.ChangeClosed, "closed"},
		{forge.ChangeMerged, "merged"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			t.Run("String", func(t *testing.T) {
				assert.Equal(t, tt.str, tt.state.String())
			})

			t.Run("MarshalRoundTrip", func(t *testing.T) {
				bs, err := tt.state.MarshalText()
				assert.NoError(t, err)

				var s forge.ChangeState
				require.NoError(t, s.UnmarshalText(bs))

				assert.Equal(t, tt.state, s)
			})
		})
	}

	t.Run("unknown", func(t *testing.T) {
		s := forge.ChangeState(42)

		t.Run("String", func(t *testing.T) {
			assert.Equal(t, "unknown", s.String())
		})

		t.Run("Marshal", func(t *testing.T) {
			_, err := s.MarshalText()
			assert.Error(t, err)
		})

		t.Run("Unmarshal", func(t *testing.T) {
			err := s.UnmarshalText([]byte("unknown"))
			assert.Error(t, err)
		})
	})
}

func TestMergeMethod(t *testing.T) {
	tests := []struct {
		method forge.MergeMethod
		text   string
	}{
		{forge.MergeMethodDefault, "default"},
		{forge.MergeMethodMerge, "merge"},
		{forge.MergeMethodSquash, "squash"},
		{forge.MergeMethodRebase, "rebase"},
	}

	for _, tt := range tests {
		t.Run(tt.method.String(), func(t *testing.T) {
			t.Run("Marshal", func(t *testing.T) {
				got, err := tt.method.MarshalText()
				require.NoError(t, err)
				assert.Equal(t, tt.text, string(got))
			})

			t.Run("Unmarshal", func(t *testing.T) {
				var got forge.MergeMethod
				require.NoError(t, got.UnmarshalText([]byte(tt.text)))
				assert.Equal(t, tt.method, got)
			})
		})
	}

	t.Run("unknown", func(t *testing.T) {
		method := forge.MergeMethod(42)

		t.Run("String", func(t *testing.T) {
			assert.Equal(t, "MergeMethod(42)", method.String())
		})

		t.Run("Marshal", func(t *testing.T) {
			_, err := method.MarshalText()
			assert.Error(t, err)
		})

		t.Run("Unmarshal", func(t *testing.T) {
			err := method.UnmarshalText([]byte("fast-forward"))
			assert.Error(t, err)
		})
	})

	t.Run("emptyDefault", func(t *testing.T) {
		var got forge.MergeMethod
		require.NoError(t, got.UnmarshalText(nil))
		assert.Equal(t, forge.MergeMethodDefault, got)
	})
}

func TestChangeMergeabilityState(t *testing.T) {
	tests := []struct {
		name   string
		give   forge.ChangeMergeabilityState
		want   string
		wantGo string
	}{
		{
			name:   "Unknown",
			give:   forge.ChangeMergeabilityUnknown,
			want:   "unknown",
			wantGo: "ChangeMergeabilityUnknown",
		},
		{
			name:   "Unsupported",
			give:   forge.ChangeMergeabilityUnsupported,
			want:   "unsupported",
			wantGo: "ChangeMergeabilityUnsupported",
		},
		{
			name:   "Ready",
			give:   forge.ChangeMergeabilityReady,
			want:   "ready",
			wantGo: "ChangeMergeabilityReady",
		},
		{
			name:   "Waiting",
			give:   forge.ChangeMergeabilityWaiting,
			want:   "waiting",
			wantGo: "ChangeMergeabilityWaiting",
		},
		{
			name:   "Blocked",
			give:   forge.ChangeMergeabilityBlocked,
			want:   "blocked",
			wantGo: "ChangeMergeabilityBlocked",
		},
		{
			name:   "Unrecognized",
			give:   forge.ChangeMergeabilityState(42),
			want:   "ChangeMergeabilityState(42)",
			wantGo: "ChangeMergeabilityState(42)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
			assert.Equal(t, tt.wantGo, tt.give.GoString())
		})
	}
}

func TestChangeMergeabilityReason(t *testing.T) {
	tests := []struct {
		name   string
		give   forge.ChangeMergeabilityReason
		want   string
		wantGo string
	}{
		{
			name:   "Unknown",
			give:   forge.ChangeMergeabilityReasonUnknown,
			want:   "unknown",
			wantGo: "ChangeMergeabilityReasonUnknown",
		},
		{
			name:   "Checks",
			give:   forge.ChangeMergeabilityReasonChecks,
			want:   "checks",
			wantGo: "ChangeMergeabilityReasonChecks",
		},
		{
			name:   "Review",
			give:   forge.ChangeMergeabilityReasonReview,
			want:   "review",
			wantGo: "ChangeMergeabilityReasonReview",
		},
		{
			name:   "Draft",
			give:   forge.ChangeMergeabilityReasonDraft,
			want:   "draft",
			wantGo: "ChangeMergeabilityReasonDraft",
		},
		{
			name:   "Conflicts",
			give:   forge.ChangeMergeabilityReasonConflicts,
			want:   "conflicts",
			wantGo: "ChangeMergeabilityReasonConflicts",
		},
		{
			name:   "Behind",
			give:   forge.ChangeMergeabilityReasonBehind,
			want:   "behind",
			wantGo: "ChangeMergeabilityReasonBehind",
		},
		{
			name:   "Discussions",
			give:   forge.ChangeMergeabilityReasonDiscussions,
			want:   "discussions",
			wantGo: "ChangeMergeabilityReasonDiscussions",
		},
		{
			name:   "Policy",
			give:   forge.ChangeMergeabilityReasonPolicy,
			want:   "policy",
			wantGo: "ChangeMergeabilityReasonPolicy",
		},
		{
			name:   "Unrecognized",
			give:   forge.ChangeMergeabilityReason(42),
			want:   "ChangeMergeabilityReason(42)",
			wantGo: "ChangeMergeabilityReason(42)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
			assert.Equal(t, tt.wantGo, tt.give.GoString())
		})
	}
}
