package shamhub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchingTemplatePaths(t *testing.T) {
	templatePathSet := makeTemplatePathSet(_changeTemplatePaths)

	tests := []struct {
		name string
		give string
		want matchingTemplatePath
		ok   bool
	}{
		{
			name: "File",
			give: "change_template.md",
			want: matchingTemplatePath{
				filePath: "change_template.md",
				filename: "change_template.md",
			},
			ok: true,
		},
		{
			name: "DirectoryFile",
			give: ".SHAMHUB/CHANGE_TEMPLATE/review.md",
			want: matchingTemplatePath{
				filePath: ".SHAMHUB/CHANGE_TEMPLATE/review.md",
				filename: "review.md",
			},
			ok: true,
		},
		{
			name: "NestedDirectoryFile",
			give: ".shamhub/change_template/nested/skip.md",
		},
		{
			name: "NonMarkdownDirectoryFile",
			give: ".shamhub/change_template.txt",
		},
		{
			name: "UnrelatedFile",
			give: "README.md",
		},
		{
			name: "Empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := matchTemplatePath(tt.give, templatePathSet)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func makeTemplatePathSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[strings.ToLower(p)] = struct{}{}
	}
	return set
}
