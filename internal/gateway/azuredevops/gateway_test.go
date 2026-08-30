package azuredevops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConnection_bearerAuthentication(t *testing.T) {
	connection := newConnection(
		"https://dev.azure.com/example",
		AuthenticationBearer,
		"token",
	)

	assert.Equal(t, "Bearer token", connection.AuthorizationString)
}
