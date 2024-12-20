package github

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestMustPR(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		assert.Equal(t, &PR{Number: 42}, mustPR(&PR{Number: 42}))
	})

	t.Run("invalid", func(t *testing.T) {
		var x struct{ forge.ChangeID }

		assert.Panics(t, func() {
			mustPR(&x)
		})
	})
}

func TestPRString(t *testing.T) {
	assert.Equal(t, "#42", (&PR{
		Number: 42,
		GQLID:  "foo",
	}).String())
}

func TestPRMarshal(t *testing.T) {
	tests := []struct {
		name string
		give PR
		want string
	}{
		{
			name: "NumberOnly",
			give: PR{Number: 42},
			want: `{"number": 42}`,
		},
		{
			name: "NumberAndGQLID",
			give: PR{Number: 42, GQLID: "foo"},
			want: `{"number": 42, "gqlID": "foo"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := new(Forge).MarshalChangeID(&tt.give)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestPRUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    PR
		wantErr string
	}{
		{
			name: "old format",
			give: "123",
			want: PR{Number: 123},
		},
		{
			name: "new format",
			give: `{"number": 123, "gqlID": "foo"}`,
			want: PR{Number: 123, GQLID: "foo"},
		},
		{
			name:    "invalid",
			give:    `"foo"`,
			wantErr: "unmarshal GitHub change ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := new(Forge).UnmarshalChangeID(json.RawMessage(tt.give))
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

func FuzzChangeMetadataMarshalRoundtrip(f *testing.F) {
	f.Add([]byte(`{"number": 123, "gqlID": "foo"}`))
	f.Add([]byte(`123`))
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
