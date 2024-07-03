package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
)

type authLogoutCmd struct{}

func (*authLogoutCmd) Help() string {
	return text.Dedent(`
		The stored authentication information is deleted from secure storage.
		Use 'gs auth login' to log in again.

		No-op if not logged in.
	`)
}

func (cmd *authLogoutCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	globals *globalOptions,
	f forge.Forge,
) error {
	if err := f.ClearAuthenticationToken(stash); err != nil {
		return err
	}

	// TOOD: Forges should present friendly names in addition to IDs.
	log.Infof("%s: logged out", f.ID())
	return nil
}
