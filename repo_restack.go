package main

import (
	"context"

	"go.abhg.dev/gs/internal/text"
)

type repoRestackCmd struct{}

func (*repoRestackCmd) Help() string {
	return text.Dedent(`
		All tracked branches in the repository are rebased on top of their
		respective bases in dependency order, ensuring a linear history.
	`)
}

func (*repoRestackCmd) Run(ctx context.Context, handler RestackHandler) error {
	return handler.RestackRepo(ctx)
}
