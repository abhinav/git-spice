package azuredevops

import (
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestMergeStrategy(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		assert.Nil(t, mergeStrategy(forge.MergeMethodDefault))
	})

	t.Run("Merge", func(t *testing.T) {
		got := mergeStrategy(forge.MergeMethodMerge)
		require.NotNil(t, got)
		assert.Equal(t, git.GitPullRequestMergeStrategyValues.NoFastForward, *got)
	})
}
