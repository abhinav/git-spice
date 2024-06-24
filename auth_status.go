package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/secret"
)

type authStatusCmd struct {
	Forge string `arg:"" help:"Service to log into" optional:"" predictors:"forges"`
}

func (cmd *authStatusCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	globals *globalOptions,
) error {
	f, err := resolveForge(ctx, log, globals, cmd.Forge)
	if err != nil {
		return err
	}

	if _, err := f.LoadAuthenticationToken(stash); err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return fmt.Errorf("not logged into %s", f.ID())
		}
		return fmt.Errorf("load authentication token: %w", err)
	}

	log.Infof("%s: currently logged in", f.ID())
	return nil
}
