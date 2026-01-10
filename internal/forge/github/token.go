package github

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/xec"
	"golang.org/x/oauth2"
)

// CLITokenSource is an oauth2 token source
// that uses the GitHub CLI to get a token.
//
// This is not super safe and we should probably nuke it.
type CLITokenSource struct {
	execer xec.Execer
}

// Token returns an oauth2 token using the GitHub CLI.
func (ts *CLITokenSource) Token() (*oauth2.Token, error) {
	ctx := context.Background()
	cmd := xec.Command(ctx, nil, "gh", "auth", "token").WithExecer(ts.execer)
	bs, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get token from gh CLI: %w", err)
	}
	return &oauth2.Token{
		AccessToken: strings.TrimSpace(string(bs)),
	}, nil
}
