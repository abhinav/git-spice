package main

import (
	"context"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/secret"
)

type authLogoutCmd struct {
	Forge string `arg:"" help:"Service to log into" optional:"" predictors:"forges"`
}

func (cmd *authLogoutCmd) Run(
	ctx context.Context,
	stash secret.Stash,
	log *log.Logger,
	globals *globalOptions,
) error {
	f, err := resolveForge(ctx, log, globals, cmd.Forge)
	if err != nil {
		return err
	}

	if err := f.ClearAuthenticationToken(stash); err != nil {
		return err
	}

	// TOOD: Forges should present friendly names in addition to IDs.
	log.Infof("%s: logged out", f.ID())
	return nil
}
