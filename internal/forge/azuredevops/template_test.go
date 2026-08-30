package azuredevops

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.uber.org/mock/gomock"
)

func TestForge_ChangeTemplatePaths_includesAdditionalTemplateDirectory(t *testing.T) {
	paths := new(Forge).ChangeTemplatePaths()
	require.NotEmpty(t, paths)

	var firstNonMarkdown string
	for _, path := range paths {
		if !hasAnySuffix(path, ".md", ".txt") {
			firstNonMarkdown = path
			break
		}
	}

	assert.Equal(t, ".azuredevops/pull_request_template", firstNonMarkdown)
}

func TestRepository_ListChangeTemplates(t *testing.T) {
	emptyContent := ""
	nonEmptyContent := "Template body\n"
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().Items(gomock.Any(), "myproject", "myrepo", gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _, _, path string) ([]azuredevops.Item, error) {
			if path != ".azuredevops/pull_request_template" {
				return nil, azuredevops.ErrNotFound
			}
			return []azuredevops.Item{
				{Path: "/.azuredevops/pull_request_template", Folder: true},
				{Path: "/.azuredevops/pull_request_template/empty.md"},
				{Path: "/.azuredevops/pull_request_template/non-empty.md"},
				{Path: "/.azuredevops/pull_request_template/branches", Folder: true},
			}, nil
		},
	)
	gateway.EXPECT().Item(gomock.Any(), "myproject", "myrepo", gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _, _, path string) (*azuredevops.Item, error) {
			switch path {
			case "/.azuredevops/pull_request_template/empty.md":
				return &azuredevops.Item{Content: emptyContent}, nil
			case "/.azuredevops/pull_request_template/non-empty.md":
				return &azuredevops.Item{Content: nonEmptyContent}, nil
			default:
				return nil, azuredevops.ErrNotFound
			}
		},
	)

	repo := newTestRepository(gateway)

	templates, err := repo.ListChangeTemplates(t.Context())
	require.NoError(t, err)

	assert.ElementsMatch(t, []*forge.ChangeTemplate{
		{Filename: "empty.md", Body: ""},
		{Filename: "non-empty.md", Body: "Template body\n"},
	}, templates)
}

// Regression test for a bug where a template found early
// (e.g. ".azuredevops/pull_request_template.md") was discarded
// if a later candidate path failed with a non-404 error
// (e.g. a timeout while listing other candidate paths).
func TestRepository_ListChangeTemplates_partialFailure(t *testing.T) {
	templateContent := "Template body\n"
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().Item(gomock.Any(), "myproject", "myrepo", gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, _, _, path string) (*azuredevops.Item, error) {
			switch path {
			case ".azuredevops/pull_request_template.md":
				return &azuredevops.Item{Content: templateContent}, nil
			case "docs/pull_request_template.md":
				return nil, context.DeadlineExceeded
			default:
				return nil, azuredevops.ErrNotFound
			}
		},
	)
	gateway.EXPECT().Items(gomock.Any(), "myproject", "myrepo", gomock.Any()).AnyTimes().DoAndReturn(
		func(context.Context, string, string, string) ([]azuredevops.Item, error) {
			return nil, azuredevops.ErrNotFound
		},
	)

	repo := newTestRepository(gateway)

	templates, err := repo.ListChangeTemplates(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []*forge.ChangeTemplate{
		{Filename: "pull_request_template.md", Body: templateContent},
	}, templates)
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}
