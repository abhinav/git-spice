package github

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphQLError_matching(t *testing.T) {
	give := graphQLError{
		{Type: "NOT_FOUND", Message: "missing"},
		{Type: "FORBIDDEN", Message: "denied"},
		{Type: "UNPROCESSABLE", Message: "invalid"},
	}

	assert.ErrorIs(t, give, ErrNotFound)
	assert.ErrorIs(t, give, ErrForbidden)
	assert.ErrorIs(t, give, ErrUnprocessable)

	var gotList graphQLError
	require.ErrorAs(t, give, &gotList)
	assert.Equal(t, give, gotList)

	var gotError *Error
	require.ErrorAs(t, give, &gotError)
	assert.Equal(t, Error{Type: "NOT_FOUND", Message: "missing"}, *gotError)
	assert.True(t, errors.Is(gotError, ErrNotFound))
}

func TestGraphQLError_Error(t *testing.T) {
	give := graphQLError{
		{
			Type:    "NOT_FOUND",
			Path:    []any{"repository", "owner"},
			Message: "missing",
		},
		{Message: "another error"},
	}

	assert.Equal(t,
		"repository.owner: NOT_FOUND: missing\nanother error",
		give.Error(),
	)
}
