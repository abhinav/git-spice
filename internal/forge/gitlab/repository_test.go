package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func TestAccessValueName(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		assert.Equal(t, "admin", accessValueName(gitlab.AdminPermissions))
	})

	t.Run("unknown", func(t *testing.T) {
		assert.Equal(t, "999", accessValueName(gitlab.AccessLevelValue(999)))
	})
}

func TestRepository_NavigationReference(t *testing.T) {
	repo := &Repository{}
	assert.Equal(t, "!42+", repo.NavigationReference(&MR{Number: 42}))
}

func TestRepository_ComparisonURL(t *testing.T) {
	r := &Repository{
		owner:  "example",
		repo:   "repo",
		forge:  &Forge{Options: Options{URL: "https://gitlab.example.com"}},
		repoID: 100,
	}

	t.Run("SameRepository", func(t *testing.T) {
		assert.Equal(t,
			"https://gitlab.example.com/example/repo/-/compare/main...feat",
			r.ComparisonURL(forge.ComparisonRequest{Base: "main", Head: "feat"}),
		)
	})

	t.Run("Fork", func(t *testing.T) {
		assert.Equal(t,
			"https://gitlab.example.com/fork/repo/-/compare/main...feat%23review?from_project_id=100",
			r.ComparisonURL(forge.ComparisonRequest{
				Base: "main",
				Head: "feat#review",
				HeadRepository: &RepositoryID{
					url:   "https://gitlab.example.com",
					owner: "fork",
					name:  "repo",
				},
			}),
		)
	})
}
