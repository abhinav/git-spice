package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alecthomas/kong"
	"github.com/google/go-github/v57/github"
	"go.abhg.dev/git-stack/internal/git"
)

type submitCmd struct {
	DryRun bool `name:"dry-run" help:"Don't actually submit the stack"`

	gh  *github.Client
	git git.Git
}

func (cmd *submitCmd) Run(app *kong.Kong, log *log.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	u, _, err := cmd.gh.Users.Get(ctx, "")
	if err != nil {
		return err
	}

	fmt.Fprintf(app.Stdout, "Hello %s!\n", u.GetName())
	return nil
}
