package shamhub

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/testing/stub"
)

// SetListChangeCommentsPageSize sets the page size for listing change comments.
// This is used to test pagination.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&_listChangeCommentsPageSize, pageSize))
}

func TestShamHub_PostComment(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 1,
				Base: &shamBranch{
					Owner: "alice",
					Repo:  "example",
				},
			},
		},
	}

	t.Run("ExplicitIDAndAutoIncrement", func(t *testing.T) {
		id, err := sh.PostComment(PostCommentRequest{
			Owner:      "alice",
			Repo:       "example",
			Change:     1,
			ID:         42,
			Body:       "review this",
			Resolvable: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 42, id)

		id, err = sh.PostComment(PostCommentRequest{
			Owner:  "alice",
			Repo:   "example",
			Change: 1,
			Body:   "next",
		})
		require.NoError(t, err)
		assert.Equal(t, 43, id)
	})

	t.Run("RejectDuplicateExplicitID", func(t *testing.T) {
		_, err := sh.PostComment(PostCommentRequest{
			Owner:  "alice",
			Repo:   "example",
			Change: 1,
			ID:     42,
			Body:   "dupe",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "comment 42 already exists")
	})

	t.Run("RejectResolvedNonResolvable", func(t *testing.T) {
		_, err := sh.PostComment(PostCommentRequest{
			Owner:    "alice",
			Repo:     "example",
			Change:   1,
			ID:       100,
			Body:     "invalid",
			Resolved: true,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "resolved comments must be resolvable")
	})
}

func TestShamHub_EditComment(t *testing.T) {
	sh := &ShamHub{
		comments: []shamComment{
			{ID: 1, Change: 1, Body: "plain"},
			{ID: 2, Change: 1, Body: "review", Resolvable: true},
		},
	}

	t.Run("SetResolved", func(t *testing.T) {
		resolved := true
		require.NoError(t, sh.EditComment(EditCommentRequest{
			ID:       2,
			Resolved: &resolved,
		}))

		comments, err := sh.ListChangeComments()
		require.NoError(t, err)
		require.Len(t, comments, 2)
		assert.Equal(t, "review", comments[1].Body)
		assert.True(t, sh.comments[1].Resolved)
		assert.True(t, sh.comments[1].Resolvable)
	})

	t.Run("ClearResolved", func(t *testing.T) {
		resolved := false
		require.NoError(t, sh.EditComment(EditCommentRequest{
			ID:       2,
			Resolved: &resolved,
		}))

		assert.False(t, sh.comments[1].Resolved)
		assert.True(t, sh.comments[1].Resolvable)
	})

	t.Run("RejectResolvedOnNonResolvable", func(t *testing.T) {
		resolved := true
		err := sh.EditComment(EditCommentRequest{
			ID:       1,
			Resolved: &resolved,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "resolved comments must be resolvable")
	})

	t.Run("RejectUnknownID", func(t *testing.T) {
		err := sh.EditComment(EditCommentRequest{ID: 999})
		require.Error(t, err)
		assert.ErrorContains(t, err, "comment 999 not found")
	})
}
