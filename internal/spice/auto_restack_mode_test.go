package spice

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoRestackMode_Kong(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    AutoRestackMode
		wantErr string
	}{
		{
			name: "Default",
			want: AutoRestackUpstack,
		},
		{
			name: "Flag",
			args: []string{"--restack"},
			want: AutoRestackUpstack,
		},
		{
			name: "Negated",
			args: []string{"--no-restack"},
			want: AutoRestackNone,
		},
		{
			name: "None",
			args: []string{"--restack=none"},
			want: AutoRestackNone,
		},
		{
			name: "Upstack",
			args: []string{"--restack=upstack"},
			want: AutoRestackUpstack,
		},
		{
			name: "False",
			args: []string{"--restack=false"},
			want: AutoRestackNone,
		},
		{
			name: "True",
			args: []string{"--restack=true"},
			want: AutoRestackUpstack,
		},
		{
			name:    "Invalid",
			args:    []string{"--restack=aboves"},
			wantErr: "expected none or upstack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd struct {
				Restack AutoRestackMode `negatable:"" default:"upstack" enum:"none,upstack"`
			}

			parser, err := kong.New(&cmd)
			require.NoError(t, err)
			_, err = parser.Parse(tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cmd.Restack)
		})
	}
}
