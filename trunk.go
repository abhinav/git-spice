package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/spice/state"
)

type trunkCmd struct {
	checkout.Options
}

func (cmd *trunkCmd) Run(
	ctx context.Context,
	store *state.Store,
	checkoutHandler CheckoutHandler,
) error {
	trunk := store.Trunk()
	return checkoutHandler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  trunk,
		Options: &cmd.Options,
	})
}
