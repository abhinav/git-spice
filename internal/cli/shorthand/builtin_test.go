package shorthand_test

import (
	"reflect"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/cli/shorthand"
)

func TestBuiltinSource(t *testing.T) {
	tests := []struct {
		name string
		app  any // CLI grammar

		want    map[string][]string
		wantErr []string // non-empty if failure is expected
	}{
		{
			name: "NoCommands",
			app:  struct{}{},
			want: map[string][]string{},
		},
		{
			name: "SingleCommand",
			app: struct {
				Commit struct{} `cmd:"" aliases:"c"`
			}{},
			want: map[string][]string{}, // no entry
		},
		{
			name: "SingleAlias",
			app: struct {
				Branch struct {
					Create struct {
						Name string `arg:""`
					} `cmd:"" aliases:"c"`
				} `cmd:"" aliases:"b"`
			}{},
			want: map[string][]string{
				"bc": {"b", "c"},
			},
		},
		{
			name: "MultipleAliases",
			app: struct {
				Branch struct {
					Create struct {
						Name string `arg:""`
					} `cmd:"" aliases:"c"`
				} `cmd:"" aliases:"b,br"`
			}{},
			want: map[string][]string{
				"bc": {"b", "c"},
			},
		},
		{
			name: "MultiLetterAlias",
			app: struct {
				Branch struct {
					Create struct {
						Name string `arg:""`
					} `cmd:"" aliases:"cr"`
				} `cmd:"" aliases:"b"`
			}{},
			want: map[string][]string{
				"bcr": {"b", "cr"},
			},
		},

		{
			name: "NoAliases",
			app: struct {
				Branch struct {
					Create struct {
						Name string `arg:""`
					} `cmd:""`
				} `cmd:""`
			}{},
			want: map[string][]string{},
		},
		{
			// If a command has an alias but one of its parents
			// does not, then there's no shorthand.
			name: "ParentMissingAlias",
			app: struct {
				Branch struct {
					Create struct {
						Name string `arg:""`
					} `cmd:"" aliases:"c"`
				} `cmd:""`
			}{},
			want: map[string][]string{},
		},
		{
			name: "ConflictingShorthand",
			app: struct {
				Branch struct {
					Create struct{} `cmd:"" aliases:"c"`
					Commit struct{} `cmd:"" aliases:"c"`
				} `cmd:"" aliases:"b"`
			}{},
			wantErr: []string{
				`shorthand "bc" for branch (b) commit (c) is already in use`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := reflect.New(reflect.TypeOf(tt.app)).Interface()
			parser, err := kong.New(cli)
			require.NoError(t, err)

			src, err := shorthand.NewBuiltin(parser.Model)
			if len(tt.wantErr) > 0 {
				require.Error(t, err)
				for _, wantErr := range tt.wantErr {
					assert.ErrorContains(t, err, wantErr)
				}
				return
			}

			require.NoError(t, err)
			got := make(map[string][]string)
			for _, key := range src.Keys() {
				var ok bool
				got[key], ok = src.ExpandShorthand(key)
				require.True(t, ok, "expand(%q)", key)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuiltinSourceNode(t *testing.T) {
	var cli struct {
		Branch struct {
			Create struct {
				Name string `arg:""`
			} `cmd:"" aliases:"c"`
		} `cmd:"" aliases:"b"`
		Commit struct {
			Create struct{} `cmd:"" aliases:"c"`
		} `cmd:"" aliases:"c"`
	}

	parser, err := kong.New(&cli)
	require.NoError(t, err)

	src, err := shorthand.NewBuiltin(parser.Model)
	require.NoError(t, err)

	_, ok := src.ExpandShorthand("bs")
	assert.False(t, ok, "no shorthand expected for 'bs'")

	_, ok = src.ExpandShorthand("bc")
	assert.True(t, ok, "shorthand 'bc' expected")
	_, ok = src.ExpandShorthand("cc")
	assert.True(t, ok, "shorthand 'cc' expected")

	bcNode := src.Node("bc")
	if assert.NotNil(t, bcNode, "expected node for 'bc'") {
		assert.Equal(t, "branch (b) create (c)", bcNode.Path())
	}
	assert.Nil(t, src.Node("foo"), "expected nil for unknown node")
}
