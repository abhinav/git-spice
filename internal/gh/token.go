// Package gh gates our access to GitHub's APIs.
package gh

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/oauth2"
)

// CLITokenSource is an oauth2 token source
// that uses the GitHub CLI to get a token.
//
// This is not super safe and we should probably nuke it.
type CLITokenSource struct{}

// Token returns an oauth2 token using the GitHub CLI.
func (ts *CLITokenSource) Token() (*oauth2.Token, error) {
	ghExe, err := exec.LookPath("gh")
	if err != nil {
		return nil, errors.New("no GitHub token provided, and gh CLI not found")
	}

	gh := exec.Command(ghExe, "auth", "token")
	bs, err := gh.Output()
	if err != nil {
		return nil, fmt.Errorf("get token from gh CLI: %w", err)
	}
	return &oauth2.Token{
		AccessToken: strings.TrimSpace(string(bs)),
	}, nil
}
