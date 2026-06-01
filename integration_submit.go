package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type integrationSubmitCmd struct {
	submitOptions
	submit.BatchOptions
}

func (*integrationSubmitCmd) Help() string {
	return text.Dedent(`
		Pushes the integration branch to the configured remote with
		--force-with-lease against the hash recorded at the previous
		successful push.

		No change request (PR) is opened for the integration branch
		itself: it is a throwaway artifact. However, the downstack
		branches of each configured tip are also submitted, so any
		branches that need to be pushed or have their CRs created or
		updated are handled in one shot.

		Once a manual submit succeeds, 'gs stack submit' and
		'gs upstack submit' will keep the integration branch in sync
		with local rebuilds.
	`) + "\n" + _submitHelp
}

func (cmd *integrationSubmitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	handler IntegrationHandler,
	submitHandler SubmitHandler,
) error {
	// Submit the downstack branches of each tip before pushing the
	// integration branch. The integration branch is logically the
	// union of these tips, so publishing it implies publishing them.
	if err := cmd.submitTipStacks(ctx, log, svc, handler, submitHandler); err != nil {
		return err
	}

	err := handler.Submit(ctx)
	if err == nil {
		log.Info("Integration branch pushed.")
		return nil
	}

	var rejected *integration.PushRejectedError
	if errors.As(err, &rejected) {
		log.Error(formatPushRejected(rejected))
		return err
	}
	return err
}

// submitTipStacks submits every branch in the downstack of any
// configured tip, in trunk-to-tip topological order, deduplicating
// branches shared between tips.
//
// Missing tips (branches that no longer exist) are silently skipped:
// the integration branch's own push will fail loudly enough if the
// configuration is broken.
func (cmd *integrationSubmitCmd) submitTipStacks(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	handler IntegrationHandler,
	submitHandler SubmitHandler,
) error {
	status, err := handler.Show(ctx)
	if err != nil {
		return err
	}
	if len(status.Tips) == 0 {
		return nil
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	branches := tipDownstackOrder(graph, status.Tips)
	if len(branches) == 0 {
		return nil
	}

	log.Infof("Submitting %d branch(es) from integration tip stacks", len(branches))
	return submitHandler.SubmitBatch(ctx, &submit.BatchRequest{
		Branches:     branches,
		Options:      &cmd.Options,
		BatchOptions: &cmd.BatchOptions,
		BranchGraph:  graph,
	})
}

// tipDownstackOrder returns the union of all tip downstacks in
// trunk-first topological order. A branch shared across multiple
// tips' downstacks appears exactly once, at the position it was
// first encountered while walking the first tip that reaches it.
func tipDownstackOrder(
	graph *spice.BranchGraph, tips []integration.TipStatus,
) []string {
	var branches []string
	seen := make(map[string]struct{})
	for _, tip := range tips {
		if tip.Missing {
			continue
		}
		downstack := slices.Collect(graph.Downstack(tip.Name))
		// Downstack is tip-first; reverse so bases come before
		// the branches that depend on them.
		slices.Reverse(downstack)
		for _, b := range downstack {
			if _, ok := seen[b]; ok {
				continue
			}
			seen[b] = struct{}{}
			branches = append(branches, b)
		}
	}
	return branches
}

// formatPushRejected renders a multi-line explanation of a
// [*integration.PushRejectedError] tailored for the user.
func formatPushRejected(e *integration.PushRejectedError) string {
	var b strings.Builder
	b.WriteString("Cannot push integration branch:\n")
	b.WriteString("  remote ")
	b.WriteString(e.Remote)
	b.WriteString("/")
	b.WriteString(e.UpstreamBranch)
	b.WriteString(" is at ")
	b.WriteString(e.RemoteHash.Short())
	b.WriteString("\n  local ")
	b.WriteString(e.Branch)
	b.WriteString(" would push ")
	b.WriteString(e.LocalHash.Short())
	b.WriteString("\n  no previously-pushed hash is recorded for this checkout\n\n")
	b.WriteString("The integration branch is a local throwaway artifact; ")
	b.WriteString("'git pull' is NOT the right move.\n\n")
	b.WriteString("Likely causes:\n")
	b.WriteString("  - You ran 'git push' directly, bypassing gs's tracking.\n")
	b.WriteString("  - The same integration branch is being pushed from another checkout.\n")
	b.WriteString("  - The spice state was reset (fresh clone, manual ref edit, rebuild).\n\n")
	b.WriteString("To resolve, either accept the remote and overwrite on the next push:\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration mark-pushed\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration submit\n")
	b.WriteString("Or start over locally:\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration delete\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration create ...\n\n")
	b.WriteString("If multiple checkouts are publishing this branch, stop. ")
	b.WriteString("It is inherently lossy.")
	return b.String()
}
