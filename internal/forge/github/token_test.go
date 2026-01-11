package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/xec/xectest"
	"go.uber.org/mock/gomock"
)

func TestCLITokenSource(t *testing.T) {
	execer := xectest.NewMockExecer(gomock.NewController(t))
	execer.EXPECT().
		Output(gomock.Any()).
		Return([]byte("mytoken\n"), nil)

	ts := &CLITokenSource{execer: execer}

	token, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, "mytoken", token.AccessToken)

	t.Run("error", func(t *testing.T) {
		execer.EXPECT().
			Output(gomock.Any()).
			Return(nil, assert.AnError)

		ts := &CLITokenSource{execer: execer}

		_, err := ts.Token()
		require.Error(t, err)
		assert.ErrorIs(t, err, assert.AnError)
	})
}
