package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
)

type authLogoutCmd struct{}

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
