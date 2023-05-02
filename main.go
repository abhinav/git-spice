// git-stack is a command line tool to manage a stack of GitHub pull requests.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/alecthomas/kong"
	"golang.org/x/oauth2"
)

func main() {
	log := log.New(os.Stderr, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		<-sigc
		log.Println("Cleaning up. Press Ctrl-C again to exit immediately.")
		cancel()
	}()

	var cmd mainCmd
	kctx := kong.Parse(
		&cmd,
		kong.Name("git stack"),
		kong.Description("git-stack is a command line tool to manage stacks of GitHub pull requests."),
		kong.Bind(log, &cmd.globalOptions),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.UsageOnError(),
	)

	kctx.FatalIfErrorf(kctx.Run())
}

type globalOptions struct {
	Token string `name:"token" env:"GITHUB_TOKEN" help:"GitHub API token; defaults to $GITHUB_TOKEN"`
}

type mainCmd struct {
	globalOptions

	// Flags with side effects that are never used directly.
	Verbose bool        `short:"v" help:"Enable verbose output"`
	Version versionFlag `help:"Print version information and quit"`

	// Commands
	SubmitCmd  submitCmd  `name:"submit" cmd:"" help:"Submit a stack of pull requests"`
	VersionCmd versionCmd `name:"version" cmd:"" help:"Print version information"`
}

func (cmd *mainCmd) AfterApply(kctx *kong.Context, log *log.Logger) error {
	if !cmd.Verbose {
		log.SetOutput(io.Discard)
	}

	var tokenSource oauth2.TokenSource = &githubCLITokenSource{}
	if token := cmd.Token; token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}

	kctx.BindTo(tokenSource, (*oauth2.TokenSource)(nil))
	return nil
}

type githubCLITokenSource struct{}

func (ts *githubCLITokenSource) Token() (*oauth2.Token, error) {
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
