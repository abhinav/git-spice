package azuredevops

import (
	"context"
	"strings"
	"testing"

	azdo "github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
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
	stub := &stubGitClient{
		getItems: func(
			_ context.Context,
			args git.GetItemsArgs,
		) (*[]git.GitItem, error) {
			require.NotNil(t, args.ScopePath)
			if *args.ScopePath != ".azuredevops/pull_request_template" {
				statusCode := 404
				return nil, azdo.WrappedError{StatusCode: &statusCode}
			}
			require.NotNil(t, args.RecursionLevel)
			assert.Equal(t,
				git.VersionControlRecursionTypeValues.OneLevel,
				*args.RecursionLevel,
			)

			isFolder := true
			isFile := false
			return &[]git.GitItem{
				{Path: new("/.azuredevops/pull_request_template"), IsFolder: &isFolder},
				{Path: new("/.azuredevops/pull_request_template/empty.md"), IsFolder: &isFile},
				{Path: new("/.azuredevops/pull_request_template/non-empty.md"), IsFolder: &isFile},
				{Path: new("/.azuredevops/pull_request_template/branches"), IsFolder: &isFolder},
			}, nil
		},
		getItem: func(
			_ context.Context,
			args git.GetItemArgs,
		) (*git.GitItem, error) {
			require.NotNil(t, args.IncludeContent)
			assert.True(t, *args.IncludeContent)

			switch *args.Path {
			case "/.azuredevops/pull_request_template/empty.md":
				return &git.GitItem{Content: &emptyContent}, nil
			case "/.azuredevops/pull_request_template/non-empty.md":
				return &git.GitItem{Content: &nonEmptyContent}, nil
			default:
				statusCode := 404
				return nil, azdo.WrappedError{StatusCode: &statusCode}
			}
		},
	}

	repo := newTestRepository(stub)

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
	stub := &stubGitClient{
		getItem: func(
			_ context.Context,
			args git.GetItemArgs,
		) (*git.GitItem, error) {
			switch *args.Path {
			case ".azuredevops/pull_request_template.md":
				return &git.GitItem{Content: &templateContent}, nil
			case "docs/pull_request_template.md":
				return nil, context.DeadlineExceeded
			default:
				statusCode := 404
				return nil, azdo.WrappedError{StatusCode: &statusCode}
			}
		},
		getItems: func(
			_ context.Context,
			_ git.GetItemsArgs,
		) (*[]git.GitItem, error) {
			statusCode := 404
			return nil, azdo.WrappedError{StatusCode: &statusCode}
		},
	}

	repo := newTestRepository(stub)

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
