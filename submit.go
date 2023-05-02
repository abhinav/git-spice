package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alecthomas/kong"
	"go.abhg.dev/git-stack/internal/git"
)

type submitCmd struct {
	DryRun bool `name:"dry-run" help:"Don't actually submit the stack"`
}

func (cmd *submitCmd) Run(
	ctx context.Context,
	app *kong.Kong,
	log *log.Logger,
	// ghtok oauth2.TokenSource,
) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	head, err := repo.HeadCommit(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD commit: %w", err)
	}

	app.Printf("HEAD commit is %s", head)
	return nil

	// ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	// defer cancel()
	//
	// gh := github.NewClient(oauth2.NewClient(ctx, ghtok))
	// u, _, err := gh.Users.Get(ctx, "")
	// if err != nil {
	// 	return err
	// }
	//
	// fmt.Fprintf(app.Stdout, "Hello %s!\n", u.GetName())
}
