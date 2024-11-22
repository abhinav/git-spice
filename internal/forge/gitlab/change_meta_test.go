package gitlab

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestMustMR(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		assert.Equal(t, &MR{Number: 42}, mustMR(&MR{Number: 42}))
	})

	t.Run("invalid", func(t *testing.T) {
		var x struct{ forge.ChangeID }

		assert.Panics(t, func() {
			mustMR(&x)
		})
	})
}

func TestMRString(t *testing.T) {
	assert.Equal(t, "!42", (&MR{
		Number: 42,
	}).String())
}

func TestMRUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    MR
		wantErr string
	}{
		{
			name: "correct format",
			give: `{"number": 123}`,
			want: MR{Number: 123},
		},
		{
			name:    "invalid",
			give:    `"foo"`,
			wantErr: "unmarshal GitLab change ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pr MR
			err := json.Unmarshal([]byte(tt.give), &pr)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, pr)
		})
	}
}

func FuzzChangeMetadataMarshalRoundtrip(f *testing.F) {
	f.Add([]byte(`{"number": 123}`))
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
