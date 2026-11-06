package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaginationItemsPerPage(t *testing.T) {
	got, err := paginationItemsPerPage(nil, 10)
	require.NoError(t, err)
	assert.Equal(t, 10, got)

	got, err = paginationItemsPerPage(&PaginationOptions{ItemsPerPage: 3}, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, got)
}

func TestPaginationItemsPerPage_rejectsOutOfRange(t *testing.T) {
	_, err := paginationItemsPerPage(&PaginationOptions{ItemsPerPage: 101}, 10)
	assert.ErrorContains(t, err, "items per page must be from 1 through 100: 101")
}
