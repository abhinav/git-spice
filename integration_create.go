package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationCreateCmd struct {
	Upstream string   `name:"upstream" placeholder:"BRANCH" help:"Upstream branch name (defaults to local name)"`
	Tips     []string `name:"tip" placeholder:"BRANCH" predictor:"trackedBranches" help:"Tip branches to include (repeat to add more)"`

	Name string `arg:"" help:"Local name of the integration branch"`
}

func (*integrationCreateCmd) Help() string {
	return text.Dedent(`
		Configures the singleton integration branch for this repo.
		The branch is materialized by sequentially merging each
		tip onto trunk; it is never given a PR and is invisible to
		'gs branch' commands.

		Use 'gs integration tip add <branch>' to add tips later,
		or pass --tip multiple times here.
	`)
}

func (cmd *integrationCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	if err := handler.Create(ctx, &integration.CreateRequest{
		Name:           cmd.Name,
		UpstreamBranch: cmd.Upstream,
		Tips:           cmd.Tips,
	}); err != nil {
		return err
	}

	log.Infof("Integration branch %q configured.", cmd.Name)
	if len(cmd.Tips) == 0 {
		log.Info("Add tips with 'gs integration tip add <branch>'.")
	} else {
		log.Info("Run 'gs integration rebuild' to materialize the branch.")
	}
	return nil
}
