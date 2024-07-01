package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
)

type authLoginCmd struct {
	Refresh bool `help:"Force a refresh of the authentication token"`
}

func (cmd *authLoginCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	globals *globalOptions,
	f forge.Forge,
) error {
	if _, err := f.LoadAuthenticationToken(stash); err == nil && !cmd.Refresh {
		log.Errorf("Use --refresh to force a refresh of the authentication token")
		return fmt.Errorf("%s: already logged in", f.ID())
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
