package submit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
	gomock "go.uber.org/mock/gomock"
)

func TestBranchSubmit_listChangeTemplates(t *testing.T) {
	t.Run("NoTimeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()
		tmpl := &forge.ChangeTemplate{}
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			ListChangeTemplates(
				gomock.Cond(func(ctx context.Context) bool {
					_, ok := ctx.Deadline()
					return assert.False(t, ok, "context should not have a deadline")
				}), gomock.Any(), gomock.Any()).
			Return([]*forge.ChangeTemplate{tmpl}, nil)

		got := listChangeTemplates(ctx, mockService, log, "origin", nil, &Options{})
		if assert.Len(t, got, 1) {
			assert.Same(t, tmpl, got[0])
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		log := silogtest.New(t)
		ctx := t.Context()

		ctrl := gomock.NewController(t)
		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			ListChangeTemplates(
				gomock.Cond(func(ctx context.Context) bool {
					_, ok := ctx.Deadline()
					return assert.True(t, ok, "context should have a deadline")
				}), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("great sadness"))

		got := listChangeTemplates(ctx, mockService, log, "origin", nil, &Options{
			ListTemplatesTimeout: time.Second,
		})
		assert.Empty(t, got)
	})
}
