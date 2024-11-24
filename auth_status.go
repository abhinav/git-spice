package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
)

type authStatusCmd struct{}

func (*authStatusCmd) Help() string {
	return `Exits with a non-zero code if not logged in.`
}

func (cmd *authStatusCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	f forge.Forge,
) error {
	if _, err := f.LoadAuthenticationToken(stash); err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return fmt.Errorf("%s: not logged in", f.ID())
		}
		return fmt.Errorf("load authentication token: %w", err)
	}

	log.Infof("%s: currently logged in", f.ID())
	return nil
}
