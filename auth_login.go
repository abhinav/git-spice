package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/secret"
)

type authLoginCmd struct {
	Forge string `arg:"" help:"Service to log into" optional:"" predictors:"forges"`

	Refresh bool `help:"Force a refresh of the authentication token"`
}

func (cmd *authLoginCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	globals *globalOptions,
) error {
	f, err := resolveForge(ctx, log, globals, cmd.Forge)
	if err != nil {
		return err
	}

	if _, err := f.LoadAuthenticationToken(stash); err == nil && !cmd.Refresh {
		log.Errorf("Already logged into %s", f.ID())
		log.Errorf("Use --refresh to force a refresh of the authentication token")
		return fmt.Errorf("already logged in")
	}

	secret, err := f.AuthenticationFlow(ctx)
	if err != nil {
		return err
	}

	if err := f.SaveAuthenticationToken(stash, secret); err != nil {
		return err
	}

	log.Infof("%s: successfully logged in", f.ID())
	return nil
}
