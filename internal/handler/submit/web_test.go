package submit

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitOpenWeb(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want OpenWeb
		arg  string
		flag bool
	}{
		{
			name: "NoFlagDefaultsToNo",
			args: []string{},
			want: OpenWebNever,
		},
		{
			name: "Web",
			args: []string{"--web"},
			want: OpenWebAlways,
		},
		{
			name: "WebTrue",
			args: []string{"--web=true"},
			want: OpenWebAlways,
		},
		{
			name: "NoWeb",
			args: []string{"--no-web"},
			want: OpenWebNever,
		},
		{
			name: "WebFalse",
			args: []string{"--web=false"},
			want: OpenWebNever,
		},
		{
			name: "WebCreated",
			args: []string{"--web=created"},
			want: OpenWebOnCreate,
		},
		{
			name: "WebWithArg",
			args: []string{"--web", "some-arg"},
			want: OpenWebAlways,
			arg:  "some-arg",
		},
		{
			name: "WebWithFlag",
			args: []string{"--web", "--flag"},
			want: OpenWebAlways,
			flag: true,
		},
		{
			name: "WebWithArgAndFlag",
			args: []string{"--web", "some-arg", "--flag"},
			want: OpenWebAlways,
			arg:  "some-arg",
			flag: true,
		},
		{
			name: "ShortWeb",
			args: []string{"-w"},
			want: OpenWebAlways,
		},
		{
			name: "ShortWebWithArg",
			args: []string{"-w", "some-arg"},
			want: OpenWebAlways,
			arg:  "some-arg",
		},
		{
			name: "ShortExplicitWeb",
			args: []string{"-w1"},
			want: OpenWebAlways,
		},
		{
			name: "ShortExplicitNoWeb",
			args: []string{"-w0"},
			want: OpenWebNever,
		},
		{
			name: "ShortExplicitCreated",
			args: []string{"-wcreated"},
			want: OpenWebOnCreate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd struct {
				Web   OpenWeb `short:"w"`
				NoWeb bool

				Flag bool   `name:"flag" help:"A flag for testing."`
				Arg  string `arg:"" optional:"" name:"arg"`
			}
			app, err := kong.New(&cmd)
			require.NoError(t, err)
			_, err = app.Parse(tt.args)
			require.NoError(t, err)

			got := cmd.Web
			if cmd.NoWeb {
				got = OpenWebNever
			}
			assert.Equal(t, got, cmd.Web)
			assert.Equal(t, tt.arg, cmd.Arg)
			assert.Equal(t, tt.flag, cmd.Flag)
		})
	}
}
