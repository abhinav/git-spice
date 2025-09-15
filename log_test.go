package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/list"
)

func TestPushStatusFormat(t *testing.T) {
	tests := []struct {
		give        string
		want        pushStatusFormat
		wantStr     string
		wantEnabled bool
	}{
		{
			give:        "true",
			want:        pushStatusEnabled,
			wantStr:     "true",
			wantEnabled: true,
		},
		{
			give:        "yes",
			want:        pushStatusEnabled,
			wantStr:     "true",
			wantEnabled: true,
		},
		{
			give:        "false",
			want:        pushStatusDisabled,
			wantStr:     "false",
			wantEnabled: false,
		},
		{
			give:        "no",
			want:        pushStatusDisabled,
			wantStr:     "false",
			wantEnabled: false,
		},
		{
			give:        "aheadbehind",
			want:        pushStatusAheadBehind,
			wantStr:     "aheadBehind",
			wantEnabled: true,
		},
		{
			give:        "aheadBehind",
			want:        pushStatusAheadBehind,
			wantStr:     "aheadBehind",
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Run("Unmarshal", func(t *testing.T) {
				var got pushStatusFormat
				err := got.UnmarshalText([]byte(tt.give))
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})

			t.Run("String", func(t *testing.T) {
				got := tt.want.String()
				assert.Equal(t, tt.wantStr, got)
			})

			t.Run("Enabled", func(t *testing.T) {
				got := tt.want.Enabled()
				assert.Equal(t, tt.wantEnabled, got)
			})
		})
	}

	t.Run("invalid", func(t *testing.T) {
		t.Run("Unmarshal", func(t *testing.T) {
			var got pushStatusFormat
			err := got.UnmarshalText([]byte("invalid"))
			require.Error(t, err)
		})

		t.Run("String", func(t *testing.T) {
			got := pushStatusFormat(999) // invalid value
			assert.Equal(t, "unknown", got.String())
		})

		t.Run("Enabled", func(t *testing.T) {
			got := pushStatusFormat(999) // invalid value
			assert.False(t, got.Enabled())
		})
	})
}

func TestChangeFormat(t *testing.T) {
	tests := []struct {
		give    string
		want    changeFormat
		wantStr string
	}{
		{
			give:    "id",
			want:    changeFormatID,
			wantStr: "id",
		},
		{
			give:    "url",
			want:    changeFormatURL,
			wantStr: "url",
		},
		{
			give:    "ID",
			want:    changeFormatID,
			wantStr: "id",
		},
		{
			give:    "URL",
			want:    changeFormatURL,
			wantStr: "url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Run("Unmarshal", func(t *testing.T) {
				var got changeFormat
				err := got.UnmarshalText([]byte(tt.give))
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})

			t.Run("String", func(t *testing.T) {
				got := tt.want.String()
				assert.Equal(t, tt.wantStr, got)
			})
		})
	}

	t.Run("invalid", func(t *testing.T) {
		t.Run("Unmarshal", func(t *testing.T) {
			var got changeFormat
			err := got.UnmarshalText([]byte("invalid"))
			require.Error(t, err)
		})

		t.Run("String", func(t *testing.T) {
			got := changeFormat(999) // invalid value
			assert.Equal(t, "changeFormat(999)", got.String())
		})
	})
}

func TestJSONLogPresenter_Present(t *testing.T) {
	t.Run("SingleBranch", func(t *testing.T) {
		var buf bytes.Buffer
		presenter := &jsonLogPresenter{Stdout: &buf}

		res := &list.BranchesResponse{
			Branches: []*list.BranchItem{
				{Name: "main"},
			},
			TrunkIdx: 0,
		}

		err := presenter.Present(res, "main")
		require.NoError(t, err)

		assert.Equal(t, `{"name":"main","current":true}
`, buf.String())
	})

	t.Run("BranchStack", func(t *testing.T) {
		var buf bytes.Buffer
		presenter := &jsonLogPresenter{Stdout: &buf}

		res := &list.BranchesResponse{
			Branches: []*list.BranchItem{
				{Name: "main"},
				{
					Name:         "feature",
					Base:         "main",
					Aboves:       []int{2},
					NeedsRestack: true,
				},
				{
					Name: "sub-feature",
					Base: "feature",
				},
			},
			TrunkIdx: 0,
		}

		err := presenter.Present(res, "feature")
		require.NoError(t, err)

		assert.Equal(t, `{"name":"main"}
{"name":"feature","current":true,"down":{"name":"main","needsRestack":true},"ups":[{"name":"sub-feature"}]}
{"name":"sub-feature","down":{"name":"feature"}}
`, buf.String())
	})

	t.Run("CompleteFeatures", func(t *testing.T) {
		var buf bytes.Buffer
		presenter := &jsonLogPresenter{Stdout: &buf}

		hash1 := git.Hash("abcdef0123456789abcdef0123456789abcdef01")
		hash2 := git.Hash("123456789abcdef0123456789abcdef012345678")

		changeID := &mockChangeID{id: "123"}

		res := &list.BranchesResponse{
			Branches: []*list.BranchItem{
				{Name: "main"},
				{
					Name:         "feature",
					Base:         "main",
					NeedsRestack: true,
					Commits: []git.CommitDetail{
						{
							Hash:       hash1,
							Subject:    "Add new feature",
							AuthorDate: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
						},
						{
							Hash:       hash2,
							Subject:    "Fix bug in feature",
							AuthorDate: time.Date(2024, 1, 16, 14, 45, 0, 0, time.UTC),
						},
					},
					ChangeID:  changeID,
					ChangeURL: "https://github.com/owner/repo/pull/123",
					PushStatus: &list.PushStatus{
						Ahead:     2,
						Behind:    1,
						NeedsPush: true,
					},
				},
			},
			TrunkIdx: 0,
		}

		err := presenter.Present(res, "feature")
		require.NoError(t, err)

		assert.Equal(t, `{"name":"main"}
{"name":"feature","current":true,"down":{"name":"main","needsRestack":true},"commits":[{"sha":"abcdef0123456789abcdef0123456789abcdef01","subject":"Add new feature"},{"sha":"123456789abcdef0123456789abcdef012345678","subject":"Fix bug in feature"}],"change":{"id":"123","url":"https://github.com/owner/repo/pull/123"},"push":{"ahead":2,"behind":1,"needsPush":true}}
`, buf.String())
	})
}

type mockChangeID struct {
	id string
}

func (m *mockChangeID) String() string {
	return m.id
}
