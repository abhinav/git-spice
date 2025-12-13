package shamhub

import (
	"testing"

	"go.abhg.dev/testing/stub"
)

// SetListChangeCommentsPageSize sets the page size for listing change comments.
// This is used to test pagination.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, pageSize))
}
