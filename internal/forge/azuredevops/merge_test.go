package azuredevops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

func TestMergeStrategy(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		assert.Equal(t, azuredevops.MergeMethodDefault, mergeStrategy(forge.MergeMethodDefault))
	})

	t.Run("Merge", func(t *testing.T) {
		got := mergeStrategy(forge.MergeMethodMerge)
		assert.Equal(t, azuredevops.MergeMethodNoFastForward, got)
	})
}
