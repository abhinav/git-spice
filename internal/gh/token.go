package gh

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/oauth2"
)

type CLITokenSource struct{}

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
