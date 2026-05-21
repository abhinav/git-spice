package main

import (
	"context"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/integration"
)

type integrationCmd struct {
	Show       integrationShowCmd       `cmd:"" help:"Show the configured integration branch" default:"withargs"`
	Create     integrationCreateCmd     `cmd:"" aliases:"c" help:"Configure the integration branch"`
	Delete     integrationDeleteCmd     `cmd:"" aliases:"d,rm" help:"Remove the integration branch configuration"`
	Checkout   integrationCheckoutCmd   `cmd:"" aliases:"co" help:"Switch to the integration branch"`
	Rebuild    integrationRebuildCmd    `cmd:"" aliases:"rb" help:"Rebuild the integration branch"`
	Submit     integrationSubmitCmd     `cmd:"" aliases:"s" help:"Push the integration branch to the remote"`
	MarkPushed integrationMarkPushedCmd `cmd:"" name:"mark-pushed" help:"Record a hash as the integration branch's last-pushed value"`
	Tip        integrationTipCmd        `cmd:"" help:"Manage the tips composing the integration branch"`
}

// IntegrationHandler implements integration branch operations.
type IntegrationHandler interface {
	Create(ctx context.Context, req *integration.CreateRequest) error
	Delete(ctx context.Context) error
	AddTip(ctx context.Context, branch string) error
	RemoveTip(ctx context.Context, branch string) error
	Show(ctx context.Context) (*integration.Status, error)
	Checkout(ctx context.Context) error
	Rebuild(ctx context.Context, opts *integration.RebuildOptions) (*integration.RebuildResult, error)
	Submit(ctx context.Context) error
	MarkPushed(ctx context.Context, hash git.Hash) error
	MaybeRebuild(ctx context.Context) error
	MaybeRebuildAndSubmit(ctx context.Context) error
	OnBranchRemoved(ctx context.Context, branch string) error
}

var _ IntegrationHandler = (*integration.Handler)(nil)
