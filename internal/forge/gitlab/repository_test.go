package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestAccessValueName(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		assert.Equal(t, "admin", accessValueName(gitlab.AdminPermissions))
	})

	t.Run("unknown", func(t *testing.T) {
		assert.Equal(t, "999", accessValueName(gitlab.AccessLevelValue(999)))
	})
}
