package bitbucket

import (
	"testing"

	"go.abhg.dev/testing/stub"
)

// SetListChangeCommentsPageSize changes the page size
// used for listing change comments.
//
// It restores the old value after the test finishes.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, pageSize))
}
