package github

import "testing"

// SetListChangeCommentsPageSize changes the page size
// used for listing change comments.
//
// It restores the old value after the test finishes.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	old := _listChangeCommentsPageSize
	_listChangeCommentsPageSize = pageSize
	t.Cleanup(func() {
		_listChangeCommentsPageSize = old
	})
}
