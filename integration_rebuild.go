package main

import (
	"context"
	"errors"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationRebuildCmd struct {
	Push        bool `name:"push" help:"Also push the integration branch after rebuilding"`
	AutoResolve bool `name:"auto-resolve" negatable:"" config:"integration.autoResolve" help:"Auto-resolve merge conflicts using the configured resolver script"`
}

func (*integrationRebuildCmd) Help() string {
	return text.Dedent(`
		Regenerates the integration branch by resetting it to trunk
		and sequentially merging each configured tip with --no-ff.
		Rerere is enabled for the duration of these merges so any
		recorded conflict resolutions are replayed automatically.

		On conflict, the merge is left in the worktree. Resolve the
		conflicting files, commit with 'git merge --continue', then
		re-run 'gs integration rebuild' (or 'gs intrb') to resume.

		With --auto-resolve (or spice.integration.autoResolve=true), a
		configured resolver script is invoked to attempt automatic
		resolution before surfacing conflicts. See the recipe for
		details on the JSON protocol the script must implement.
	`)
}

func (cmd *integrationRebuildCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	res, err := handler.Rebuild(ctx, &integration.RebuildOptions{
		AutoResolve: &cmd.AutoResolve,
	})
	if err != nil {
		conflict := new(integration.ConflictError)
		if errors.As(err, &conflict) {
			var b strings.Builder
			b.WriteString("Merge conflict in tip ")
			b.WriteString(conflict.Tip)
			b.WriteString(":\n")
			for _, p := range conflict.Paths {
				b.WriteString("  - ")
				b.WriteString(p)
				b.WriteString("\n")
			}
			b.WriteString("Resolve the conflicts, then run:\n")
			b.WriteString("  git merge --continue\n")
			b.WriteString("Then resume the rebuild with:\n")
			b.WriteString("  " + cli.Name() + " integration rebuild")
			log.Error(b.String())
			return err
		}
		return err
	}
	log.Infof("Integration branch %q rebuilt with %d tip(s).",
		res.Name, len(res.TipHashes))

	if cmd.Push {
		if err := handler.Submit(ctx); err != nil {
			return err
		}
		log.Infof("Integration branch %q pushed.", res.Name)
	}
	return nil
}
