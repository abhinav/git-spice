package main

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type integrationTipCmd struct {
	Add    integrationTipAddCmd    `cmd:"" aliases:"a" help:"Add a branch to the integration tip list"`
	Remove integrationTipRemoveCmd `cmd:"" aliases:"r,rm" help:"Remove a branch from the integration tip list"`
	List   integrationTipListCmd   `cmd:"" aliases:"l,ls" help:"List the configured integration tips"`
	Clean  integrationTipCleanCmd  `cmd:"" aliases:"prune" help:"Remove tips whose upstack already contains another tip"`
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

type integrationTipCleanCmd struct{}

func (*integrationTipCleanCmd) Help() string {
	return text.Dedent(`
		Removes tips whose upstack chain already contains another
		configured tip. The higher tip's merge into the integration
		branch captures the lower tip's content, so keeping both
		costs an extra merge without changing the result.

		For each subsumed tip, the message reports which higher tip
		subsumes it. A second run is a no-op once nothing remains
		to prune. Existing tips with no upstack-tip relationship
		are left alone.
	`)
}

func (cmd *integrationTipCleanCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	handler IntegrationHandler,
) error {
	status, err := handler.Show(ctx)
	if err != nil {
		return err
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	subsumed := subsumedTips(graph, status.Tips)
	if len(subsumed) == 0 {
		log.Info("No subsumed tips to prune.")
		return nil
	}

	for _, s := range subsumed {
		if err := handler.RemoveTip(ctx, s.tip); err != nil {
			return fmt.Errorf("remove tip %q: %w", s.tip, err)
		}
		log.Infof("Removed %q (subsumed by %q).", s.tip, s.subsumer)
	}
	log.Infof("Pruned %d tip(s).", len(subsumed))
	return nil
}

// subsumption records that tip is fully contained in the upstack of
// subsumer, both of which are configured integration tips.
type subsumption struct {
	tip      string
	subsumer string
}

// subsumedTips returns the set of tips that should be pruned because
// they are upstack-reachable from another configured tip.
//
// A tip T is subsumed when at least one branch above T (in the
// inclusive upstack of T, excluding T itself) is also a configured
// tip. The subsumer reported is the first such tip found while
// walking T's upstack; with cleanly-stacked branches this is stable.
//
// Tips whose branches are missing from the graph are ignored.
func subsumedTips(
	graph *spice.BranchGraph,
	tips []integration.TipStatus,
) []subsumption {
	tipSet := make(map[string]struct{}, len(tips))
	for _, t := range tips {
		if t.Missing {
			continue
		}
		tipSet[t.Name] = struct{}{}
	}

	var out []subsumption
	for _, t := range tips {
		if t.Missing {
			continue
		}
		for above := range graph.Upstack(t.Name) {
			if above == t.Name {
				continue
			}
			if _, ok := tipSet[above]; ok {
				out = append(out, subsumption{tip: t.Name, subsumer: above})
				break
			}
		}
	}
	return out
}
