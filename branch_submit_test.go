package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/logtest"
)

func TestBranchSubmit_listChangeTemplates(t *testing.T) {
	t.Run("NoTimeout", func(t *testing.T) {
		log := logtest.New(t)
		ctx := context.Background()
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
		log := logtest.New(t)
		ctx := context.Background()

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

type spiceTemplateServiceStub struct {
	ListChangeTemplatesF func(context.Context, string, forge.Repository) ([]*forge.ChangeTemplate, error)
}

func (s *spiceTemplateServiceStub) ListChangeTemplates(ctx context.Context, remoteName string, fr forge.Repository) ([]*forge.ChangeTemplate, error) {
	return s.ListChangeTemplatesF(ctx, remoteName, fr)
}
