package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type integrationTipCmd struct {
	Add     integrationTipAddCmd     `cmd:"" aliases:"a" help:"Add a branch to the integration tip list"`
	Remove  integrationTipRemoveCmd  `cmd:"" aliases:"r,rm" help:"Remove a branch from the integration tip list"`
	List    integrationTipListCmd    `cmd:"" aliases:"l,ls" help:"List the configured integration tips"`
	Clean   integrationTipCleanCmd   `cmd:"" aliases:"prune" help:"Remove tips whose upstack already contains another tip"`
	Advance integrationTipAdvanceCmd `cmd:"" help:"Move tips to the topmost branches of their upstacks"`
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

type integrationTipAdvanceCmd struct {
	Branches []string `arg:"" optional:"" predictor:"integrationTips" help:"Tips to advance; defaults to all configured tips"`
}

func (*integrationTipAdvanceCmd) Help() string {
	return text.Dedent(`
		For each configured tip (or only the named tips, if any are
		given as arguments), replaces the tip with the topmost
		branch(es) in its upstack — the leaves of the tree above it.

		If a tip already has no branches above it, it is left alone.
		If a tip's upstack forks, the tip expands to every leaf of
		the fork. Branches that would become duplicates of other
		configured tips are deduplicated.

		Useful as a one-shot maintenance step after extending a stack
		above a configured tip: instead of 'tip remove old' + 'tip
		add new', a single 'tip advance' walks each tip to its
		current top.
	`)
}

func (cmd *integrationTipAdvanceCmd) Run(
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

	scope := scopeOfTips(status.Tips, cmd.Branches)
	if err := validateAdvanceScope(scope, cmd.Branches); err != nil {
		return err
	}

	plan := advanceTips(graph, status.Tips, scope)
	if len(plan) == 0 {
		log.Info("No tips to advance.")
		return nil
	}

	for _, step := range plan {
		if err := handler.RemoveTip(ctx, step.from); err != nil {
			return fmt.Errorf("remove tip %q: %w", step.from, err)
		}
		for _, leaf := range step.to {
			if err := handler.AddTip(ctx, leaf); err != nil {
				return fmt.Errorf("add tip %q: %w", leaf, err)
			}
		}
		log.Infof("Advanced %q to %s.", step.from, formatLeaves(step.to))
	}
	return nil
}

// scopeOfTips returns the set of tip names to advance. An empty
// requested slice selects all currently-configured tips.
func scopeOfTips(tips []integration.TipStatus, requested []string) map[string]struct{} {
	scope := make(map[string]struct{})
	if len(requested) == 0 {
		for _, t := range tips {
			scope[t.Name] = struct{}{}
		}
		return scope
	}
	for _, r := range requested {
		scope[r] = struct{}{}
	}
	return scope
}

// validateAdvanceScope ensures every name in requested is actually a
// configured tip; an unrecognized name aborts before any mutation.
func validateAdvanceScope(scope map[string]struct{}, requested []string) error {
	if len(requested) == 0 {
		return nil
	}
	configured := make(map[string]struct{}, len(scope))
	for k := range scope {
		configured[k] = struct{}{}
	}
	for _, r := range requested {
		if _, ok := configured[r]; !ok {
			return fmt.Errorf("tip %q is not configured", r)
		}
	}
	return nil
}

// advanceStep records that a configured tip should be replaced with
// the listed leaves.
type advanceStep struct {
	from string
	to   []string
}

// advanceTips computes the replacement plan. For each tip in scope:
//   - If the tip is missing from the graph, skip it.
//   - Collect the leaves of its upstack via [BranchGraph.Tops].
//   - If the only leaf is the tip itself, the tip is already at
//     the top and is left alone.
//   - Otherwise replace it with all leaves, dropping leaves that
//     are already configured as other tips (or would duplicate
//     leaves already emitted by an earlier step in the plan).
//
// The plan preserves the configured tip order so output is stable.
func advanceTips(
	graph *spice.BranchGraph,
	tips []integration.TipStatus,
	scope map[string]struct{},
) []advanceStep {
	existing := make(map[string]struct{}, len(tips))
	for _, t := range tips {
		existing[t.Name] = struct{}{}
	}

	var plan []advanceStep
	for _, t := range tips {
		if _, ok := scope[t.Name]; !ok {
			continue
		}
		if t.Missing {
			continue
		}
		var leaves []string
		seen := map[string]struct{}{}
		for leaf := range graph.Tops(t.Name) {
			if _, dup := seen[leaf]; dup {
				continue
			}
			seen[leaf] = struct{}{}
			leaves = append(leaves, leaf)
		}
		if len(leaves) == 0 {
			continue
		}
		if len(leaves) == 1 && leaves[0] == t.Name {
			continue // already at the top
		}
		// Filter out leaves that would duplicate other configured
		// tips. Removing t and adding such a leaf would just collide
		// with an existing entry.
		filtered := leaves[:0]
		for _, leaf := range leaves {
			if leaf == t.Name {
				continue
			}
			if _, ok := existing[leaf]; ok {
				continue
			}
			filtered = append(filtered, leaf)
		}
		if len(filtered) == 0 {
			continue
		}
		// Update existing so subsequent steps don't emit duplicates.
		delete(existing, t.Name)
		for _, leaf := range filtered {
			existing[leaf] = struct{}{}
		}
		plan = append(plan, advanceStep{from: t.Name, to: filtered})
	}
	return plan
}

// formatLeaves renders a leaf list for the log line.
func formatLeaves(leaves []string) string {
	switch len(leaves) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%q", leaves[0])
	}
	var sb strings.Builder
	for i, leaf := range leaves {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q", leaf)
	}
	return sb.String()
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
