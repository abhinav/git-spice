package azuredevops

import (
	"errors"
	"net/http"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeError_notFound(t *testing.T) {
	statusCode := http.StatusNotFound
	sdkError := azuredevops.WrappedError{StatusCode: &statusCode}

	err := normalizeError(sdkError)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNormalizeError_otherError(t *testing.T) {
	want := errors.New("request failed")

	assert.ErrorIs(t, normalizeError(want), want)
}
