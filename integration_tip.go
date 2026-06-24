package main

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationTipCmd struct {
	Add    integrationTipAddCmd    `cmd:"" aliases:"a" help:"Add a branch to the integration tip list"`
	Remove integrationTipRemoveCmd `cmd:"" aliases:"r,rm" help:"Remove a branch from the integration tip list"`
	List   integrationTipListCmd   `cmd:"" aliases:"l,ls" help:"List the configured integration tips"`
}

type integrationTipAddCmd struct {
	Branches []string `arg:"" predictor:"trackedBranches" help:"Branches to add as tips"`
}

func (*integrationTipAddCmd) Help() string {
	return text.Dedent(`
		Adds one or more tracked branches to the integration tip list.
		Each branch must already be tracked by git-spice; this command
		does not track new branches.

		Branches are added in order. If one fails to add, the previous
		ones remain in the tip list.
	`)
}

func (cmd *integrationTipAddCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	for _, branch := range cmd.Branches {
		if err := handler.AddTip(ctx, branch); err != nil {
			return err
		}
		log.Infof("Added %q to integration tips.", branch)
	}
	return nil
}

type integrationTipRemoveCmd struct {
	Branches []string `arg:"" predictor:"integrationTips" help:"Branches to remove from the tip list"`
}

func (cmd *integrationTipRemoveCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	for _, branch := range cmd.Branches {
		if err := handler.RemoveTip(ctx, branch); err != nil {
			return err
		}
		log.Infof("Removed %q from integration tips.", branch)
	}
	return nil
}

type integrationTipListCmd struct{}

func (cmd *integrationTipListCmd) Run(
	ctx context.Context,
	kctx *kong.Context,
	handler IntegrationHandler,
) error {
	status, err := handler.Show(ctx)
	if err != nil {
		return err
	}
	for _, tip := range status.Tips {
		fmt.Fprintln(kctx.Stdout, tip.Name)
	}
	return nil
}
