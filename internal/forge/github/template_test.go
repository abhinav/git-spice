package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChangeTemplatePaths_containsLowerCaseVersion(t *testing.T) {
	// This is a silly test, but at minimum it will prevent a regression.
	paths := new(Forge).ChangeTemplatePaths()
	assert.Contains(t, paths, ".github/pull_request_template.md")
}
