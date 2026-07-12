package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateway_AddAssigneesToAssignable(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"addAssigneesToAssignable": {}}
	}`)
	require.NoError(t, gateway.AddAssigneesToAssignable(t.Context(), "PR_1", []ID{"U_1"}))
}
