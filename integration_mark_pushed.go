package main

import (
	"context"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationMarkPushedCmd struct {
	Hash string `arg:"" optional:"" help:"Commit hash to record as last-pushed. If empty, fetches the configured push remote and uses its current tip."`
}

func (*integrationMarkPushedCmd) Help() string {
	return text.Dedent(`
		Records the given commit hash as the integration branch's
		last-pushed value in gs's local state. Does not push.

		Used to reconcile state after a manual git push of the
		integration branch, or after a "push rejected" error caused by
		a multi-checkout collision or a state reset.

		With no argument, the hash is discovered from the configured
		push remote. With an explicit hash, that hash is recorded
		verbatim.

		After 'gs integration mark-pushed', the next
		'gs integration submit' uses --force-with-lease against the
		recorded hash. If multiple checkouts are publishing this
		branch, this command will not save you from a collision; it
		just confirms which remote state you accept as your baseline
		before overwriting.
	`)
}

func (cmd *integrationMarkPushedCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	if err := handler.MarkPushed(ctx, git.Hash(cmd.Hash)); err != nil {
		return err
	}
	log.Info("Recorded integration branch last-pushed hash.")
	return nil
}
