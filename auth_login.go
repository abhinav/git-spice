package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type authLoginCmd struct {
	Refresh bool `help:"Force a refresh of the authentication token"`
}

func (*authLoginCmd) Help() string {
	return text.Dedent(`
		For GitHub, a prompt will allow selecting between
		OAuth, GitHub App, and Personal Access Token-based authentication.
		The differences between them are explained in the prompt.

		The authentication token is stored in a system-provided secure storage.
		Use 'gs auth logout' to log out and delete the token from storage.

		Fails if already logged in.
		Use --refresh to force a refresh of the authentication token,
		or change the authentication method.
	`)
}

func (cmd *authLoginCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *silog.Logger,
	view ui.View,
	f forge.Forge,
) error {
	if _, err := f.LoadAuthenticationToken(stash); err == nil && !cmd.Refresh {
		log.Errorf("Use --refresh to force a refresh of the authentication token")
		return fmt.Errorf("%s: already logged in", f.ID())
	}

	secret, err := f.AuthenticationFlow(ctx, view)
	if err != nil {
		return err
	}

	if err := f.SaveAuthenticationToken(stash, secret); err != nil {
		return err
	}

	log.Infof("%s: successfully logged in", f.ID())
	return nil
}
