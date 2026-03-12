package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func TestAccessValueName(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		assert.Equal(t, "admin", accessValueName(gitlab.AdminPermissions))
	})

	t.Run("unknown", func(t *testing.T) {
		assert.Equal(t, "999", accessValueName(gitlab.AccessLevelValue(999)))
	})
}
