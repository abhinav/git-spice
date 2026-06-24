package main

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationDeleteCmd struct{}

func (*integrationDeleteCmd) Help() string {
	return text.Dedent(`
		Removes the integration branch configuration. The underlying
		Git branch (if any) is not deleted; only the git-spice config
		that drives auto-rebuild and submit is removed.
	`)
}

func (cmd *integrationDeleteCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	if err := handler.Delete(ctx); err != nil {
		return err
	}
	log.Info("Integration branch configuration removed.")
	return nil
}
