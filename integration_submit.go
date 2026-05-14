package main

import (
	"context"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationSubmitCmd struct{}

func (*integrationSubmitCmd) Help() string {
	return text.Dedent(`
		Pushes the integration branch to the configured remote with
		--force-with-lease against the hash recorded at the previous
		successful push.

		No change request (PR) is opened: this command only pushes the
		branch. Once a manual submit succeeds, 'gs stack submit' and
		'gs upstack submit' will keep the published branch in sync with
		local rebuilds.
	`)
}

func (cmd *integrationSubmitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	if err := handler.Submit(ctx); err != nil {
		return err
	}
	log.Info("Integration branch pushed.")
	return nil
}
