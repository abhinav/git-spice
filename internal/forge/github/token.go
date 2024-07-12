package github

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/oauth2"
)

// CLITokenSource is an oauth2 token source
// that uses the GitHub CLI to get a token.
//
// This is not super safe and we should probably nuke it.
type CLITokenSource struct {
	cmdOutput func(*exec.Cmd) ([]byte, error) // for testing
}

// Token returns an oauth2 token using the GitHub CLI.
func (ts *CLITokenSource) Token() (*oauth2.Token, error) {
	cmdOutput := (*exec.Cmd).Output
	if ts.cmdOutput != nil {
		cmdOutput = ts.cmdOutput
	}

	bs, err := cmdOutput(exec.Command("gh", "auth", "token"))
	if err != nil {
		return nil, fmt.Errorf("get token from gh CLI: %w", err)
	}
	return &oauth2.Token{
		AccessToken: strings.TrimSpace(string(bs)),
	}, nil
}
