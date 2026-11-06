package azuredevops

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestMustPR(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		assert.Equal(t, &PR{Number: 42}, mustPR(&PR{Number: 42}))
	})

	t.Run("Invalid", func(t *testing.T) {
		var x struct{ forge.ChangeID }

		assert.Panics(t, func() {
			mustPR(&x)
		})
	})
}

func TestPR_String(t *testing.T) {
	assert.Equal(t, "!42", (&PR{Number: 42}).String())
}

func TestPRMarshal(t *testing.T) {
	got, err := new(Forge).MarshalChangeID(&PR{Number: 42})
	require.NoError(t, err)
	assert.JSONEq(t, `{"number": 42}`, string(got))
}

func TestPRUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    PR
		wantErr string
	}{
		{
			name: "Valid",
			give: `{"number": 123}`,
			want: PR{Number: 123},
		},
		{
			name:    "InvalidJSON",
			give:    `"foo"`,
			wantErr: "unmarshal PR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := new(Forge).UnmarshalChangeID(jsontext.Value(tt.give))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, &tt.want, pr)
		})
	}
}

func TestMustPRComment(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		c := &PRComment{PRID: 42, ThreadID: 1, CommentID: 2}
		assert.Equal(t, c, mustPRComment(c))
	})

	t.Run("Nil", func(t *testing.T) {
		assert.Nil(t, mustPRComment(nil))
	})

	t.Run("Invalid", func(t *testing.T) {
		var x struct{ forge.ChangeCommentID }

		assert.Panics(t, func() {
			mustPRComment(&x)
		})
	})
}

func TestPRComment_String(t *testing.T) {
	assert.Equal(t,
		"pr-42-thread-1-comment-2",
		(&PRComment{PRID: 42, ThreadID: 1, CommentID: 2}).String(),
	)
}

func TestPRMetadata(t *testing.T) {
	t.Run("ForgeID", func(t *testing.T) {
		md := &PRMetadata{}
		assert.Equal(t, "azuredevops", md.ForgeID())
	})

	t.Run("ChangeID", func(t *testing.T) {
		pr := &PR{Number: 42}
		md := &PRMetadata{PR: pr}
		assert.Equal(t, pr, md.ChangeID())
	})

	t.Run("NavigationCommentID", func(t *testing.T) {
		t.Run("Nil", func(t *testing.T) {
			md := &PRMetadata{}
			assert.Nil(t, md.NavigationCommentID())
		})

		t.Run("Set", func(t *testing.T) {
			c := &PRComment{PRID: 42, ThreadID: 1, CommentID: 2}
			md := &PRMetadata{NavigationComment: c}
			assert.Equal(t, c, md.NavigationCommentID())
		})
	})

	t.Run("SetNavigationCommentID", func(t *testing.T) {
		md := &PRMetadata{}
		c := &PRComment{PRID: 42, ThreadID: 1, CommentID: 2}
		md.SetNavigationCommentID(c)
		assert.Equal(t, c, md.NavigationComment)
	})
}

func TestPRMetadataMarshal(t *testing.T) {
	md := &PRMetadata{
		PR:                &PR{Number: 42},
		NavigationComment: &PRComment{PRID: 42, ThreadID: 1, CommentID: 2},
	}

	got, err := new(Forge).MarshalChangeMetadata(md)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"pr":{"number":42},"comment":{"prId":42,"threadId":1,"commentId":2}}`,
		string(got),
	)
}

func TestPRMetadataUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    *PRMetadata
		wantErr string
	}{
		{
			name: "PROnly",
			give: `{"pr":{"number":42}}`,
			want: &PRMetadata{PR: &PR{Number: 42}},
		},
		{
			name: "WithComment",
			give: `{"pr":{"number":42},"comment":{"prId":42,"threadId":1,"commentId":2}}`,
			want: &PRMetadata{
				PR:                &PR{Number: 42},
				NavigationComment: &PRComment{PRID: 42, ThreadID: 1, CommentID: 2},
			},
		},
		{
			name:    "InvalidJSON",
			give:    `"foo"`,
			wantErr: "unmarshal PR metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md, err := new(Forge).UnmarshalChangeMetadata(jsontext.Value(tt.give))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, md)
		})
	}
}

func FuzzChangeMetadataMarshalRoundtrip(f *testing.F) {
	f.Add([]byte(`{"pr":{"number":123}}`))
	f.Add([]byte(`{"pr":{"number":42},"comment":{"prId":42,"threadId":1,"commentId":2}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var forge Forge

		origMD, err := forge.UnmarshalChangeMetadata(data)
		if err != nil {
			t.Skip(err)
		}

		bs, err := forge.MarshalChangeMetadata(origMD)
		require.NoError(t, err)

		md, err := forge.UnmarshalChangeMetadata(bs)
		require.NoError(t, err)

		assert.Equal(t, origMD, md)
	})
}
