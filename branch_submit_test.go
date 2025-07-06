package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestBranchSubmit_listChangeTemplates(t *testing.T) {
	t.Run("NoTimeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()
		tmpl := &forge.ChangeTemplate{}
		svc := &spiceTemplateServiceStub{
			ListChangeTemplatesF: func(ctx context.Context, _ string, _ forge.Repository) ([]*forge.ChangeTemplate, error) {
				_, ok := ctx.Deadline()
				require.False(t, ok, "context should not have a deadline")

				return []*forge.ChangeTemplate{tmpl}, nil
			},
		}

		got := (&branchSubmitCmd{}).listChangeTemplates(ctx, log, svc, "origin", nil)
		if assert.Len(t, got, 1) {
			assert.Same(t, tmpl, got[0])
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()

		svc := &spiceTemplateServiceStub{
			ListChangeTemplatesF: func(ctx context.Context, _ string, _ forge.Repository) ([]*forge.ChangeTemplate, error) {
				_, ok := ctx.Deadline()
				require.True(t, ok, "context should have a deadline")
				return nil, errors.New("great sadness")
			},
		}

		got := (&branchSubmitCmd{
			ListTemplatesTimeout: time.Second,
		}).listChangeTemplates(ctx, log, svc, "origin", nil)
		assert.Empty(t, got)
	})
}

func TestSubmitOpenWeb(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want submitOpenWeb
		arg  string
		flag bool
	}{
		{
			name: "NoFlagDefaultsToNo",
			args: []string{},
			want: submitOpenWebNo,
		},
		{
			name: "Web",
			args: []string{"--web"},
			want: submitOpenWebYes,
		},
		{
			name: "WebTrue",
			args: []string{"--web=true"},
			want: submitOpenWebYes,
		},
		{
			name: "NoWeb",
			args: []string{"--no-web"},
			want: submitOpenWebNo,
		},
		{
			name: "WebFalse",
			args: []string{"--web=false"},
			want: submitOpenWebNo,
		},
		{
			name: "WebCreated",
			args: []string{"--web=created"},
			want: submitOpenWebCreated,
		},
		{
			name: "WebWithArg",
			args: []string{"--web", "some-arg"},
			want: submitOpenWebYes,
			arg:  "some-arg",
		},
		{
			name: "WebWithFlag",
			args: []string{"--web", "--flag"},
			want: submitOpenWebYes,
			flag: true,
		},
		{
			name: "WebWithArgAndFlag",
			args: []string{"--web", "some-arg", "--flag"},
			want: submitOpenWebYes,
			arg:  "some-arg",
			flag: true,
		},
		{
			name: "ShortWeb",
			args: []string{"-w"},
			want: submitOpenWebYes,
		},
		{
			name: "ShortWebWithArg",
			args: []string{"-w", "some-arg"},
			want: submitOpenWebYes,
			arg:  "some-arg",
		},
		{
			name: "ShortExplicitWeb",
			args: []string{"-w1"},
			want: submitOpenWebYes,
		},
		{
			name: "ShortExplicitNoWeb",
			args: []string{"-w0"},
			want: submitOpenWebNo,
		},
		{
			name: "ShortExplicitCreated",
			args: []string{"-wcreated"},
			want: submitOpenWebCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd struct {
				Web   submitOpenWeb `short:"w"`
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
				got = submitOpenWebNo
			}
			assert.Equal(t, got, cmd.Web)
			assert.Equal(t, tt.arg, cmd.Arg)
			assert.Equal(t, tt.flag, cmd.Flag)
		})
	}
}

type spiceTemplateServiceStub struct {
	ListChangeTemplatesF func(context.Context, string, forge.Repository) ([]*forge.ChangeTemplate, error)
}

func (s *spiceTemplateServiceStub) ListChangeTemplates(ctx context.Context, remoteName string, fr forge.Repository) ([]*forge.ChangeTemplate, error) {
	return s.ListChangeTemplatesF(ctx, remoteName, fr)
}
