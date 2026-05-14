package main

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationCheckoutCmd struct{}

func (*integrationCheckoutCmd) Help() string {
	return text.Dedent(`
		Switches the worktree to the configured integration branch.
		Fails if no integration is configured, or if the integration
		branch has not yet been materialized (run 'gs integration
		rebuild' first).
	`)
}

func (cmd *integrationCheckoutCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	if err := handler.Checkout(ctx); err != nil {
		return err
	}
	log.Info("Switched to integration branch.")
	return nil
}
