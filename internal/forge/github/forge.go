// Package github defines a GitHub Forge.
package github

import (
	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
)

// Forge provides access to GitHub's API,
// while complying with the Forge interface.
type Forge struct {
	owner, repo string
	log         *log.Logger
	client      *github.Client
}
