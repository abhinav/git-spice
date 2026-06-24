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
	Push                bool `name:"push" help:"Also push the integration branch after rebuilding"`
	AutoResolve         bool `name:"auto-resolve" negatable:"" config:"integration.autoResolve" help:"Auto-resolve merge conflicts using the configured resolver script"`
	AcceptIncoming      bool `name:"accept-incoming" negatable:"" default:"true" config:"integration.acceptIncoming" help:"Final-stage fallback: take the incoming tip's version for any remaining conflicts so the rebuild completes without manual intervention"`
	NoRerere            bool `name:"no-rerere" help:"Disable rerere entirely (no replay AND no recording) for this rebuild. Diagnostic mode — prefer --reset-rerere-cache when you want fresh resolutions cached for next time."`
	ResetRerereCache    bool `name:"reset-rerere-cache" help:"Wipe the rerere cache (.git/rr-cache) before starting so stale cached resolutions are not replayed. Rerere stays enabled so the rebuild's fresh resolutions are recorded."`
	ResetResolutionFile bool `name:"reset-resolution-file" help:"Delete the resolution file (.integration_resolution.json) before starting so stale Q&A history is not carried into this rebuild."`
	ResetPending        bool `name:"reset-pending" help:"Clear any pending rebuild state before starting. Use when a prior halted rebuild left stale state that should not be resumed."`
	FromScratch         bool `name:"from-scratch" help:"Shorthand: implies --reset-rerere-cache, --reset-resolution-file, and --reset-pending. Use after a bad rebuild left stale state in any cache; rerere stays enabled so good resolutions get recorded."`
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

		If the resolver produces corrupt or unusable output (script
		exit failure, malformed JSON, missing markers) the rebuild
		halts rather than falling through to accept-incoming. The
		usual cause is a prompt, model, or script issue that needs
		to be fixed, not glossed over by silently picking 'theirs'.

		Conflicts that survive the merge drivers and a successful
		resolver run are auto-resolved by taking the incoming tip's
		version. Pass --no-accept-incoming (or set
		spice.integration.acceptIncoming=false) to disable that
		final fallback and surface conflicts for manual resolution
		instead.

		If a bad resolution was silently cached (in rerere, in the
		resolution file, or in pending rebuild state) and is being
		replayed on every rebuild, use --reset-rerere-cache to wipe
		stale postimages (while still recording fresh ones),
		--reset-resolution-file to wipe the Q&A history,
		--reset-pending to drop any stale resume point, or
		--from-scratch for all three.

		--no-rerere is a stronger diagnostic mode: it disables
		rerere recording too, so the rebuild produces nothing
		cached for next time. Use it only when you suspect rerere
		itself is misbehaving — otherwise --reset-rerere-cache is
		what you want.
	`)
}

func (cmd *integrationRebuildCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	resetResolution := cmd.ResetResolutionFile || cmd.FromScratch
	resetPending := cmd.ResetPending || cmd.FromScratch
	resetRerere := cmd.ResetRerereCache || cmd.FromScratch
	res, err := handler.Rebuild(ctx, &integration.RebuildOptions{
		AutoResolve:         &cmd.AutoResolve,
		AcceptIncoming:      &cmd.AcceptIncoming,
		NoRerere:            cmd.NoRerere,
		ResetResolutionFile: resetResolution,
		ResetPending:        resetPending,
		ResetRerereCache:    resetRerere,
	})
	if err != nil {
		resolverFail := new(integration.ResolverFailedError)
		if errors.As(err, &resolverFail) {
			var b strings.Builder
			b.WriteString("Resolver failed for tip ")
			b.WriteString(resolverFail.Tip)
			b.WriteString(":\n  ")
			b.WriteString(resolverFail.Cause.Error())
			b.WriteString("\nConflicted paths (left in worktree):\n")
			for _, p := range resolverFail.Paths {
				b.WriteString("  - ")
				b.WriteString(p)
				b.WriteString("\n")
			}
			b.WriteString(
				"Halting on resolver failure to avoid silently dropping " +
					"declarations from the integration side. Investigate the " +
					"resolver script (spice.integration.resolver) — a corrupt " +
					"response usually means the prompt or model is wrong.\n")
			b.WriteString("To proceed manually, resolve the conflicts, then run:\n")
			b.WriteString("  git merge --continue\n")
			b.WriteString("Then resume the rebuild with:\n")
			b.WriteString("  " + cli.Name() + " integration rebuild")
			log.Error(b.String())
			return err
		}

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
	if res.RegeneratorError != nil {
		log.Errorf(
			"Regenerator failed during rebuild: %v. The integration "+
				"branch's source is correct but its generated files "+
				"may be stale; run your project's generate target "+
				"before relying on the build.",
			res.RegeneratorError)
	}

	if cmd.Push {
		if err := handler.Submit(ctx); err != nil {
			return err
		}
		log.Infof("Integration branch %q pushed.", res.Name)
	}
	return nil
}
