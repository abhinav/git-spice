package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type ciMergeGuardCmd struct {
	Number int    `arg:"" help:"Change request number to check"`
	Trunk  string `help:"Override trunk branch name"`
	All    bool   `help:"Block all non-trunk-based PRs, not just git-spice managed ones"`
}

func (*ciMergeGuardCmd) Help() string {
	return text.Dedent(`
		Checks whether a change request is safe to merge
		by verifying its base branch is trunk.

		Use this in forge CI/CD pipelines to prevent
		out-of-order merges in a stacked PR workflow.

		By default, only git-spice managed PRs are checked.
		Unmanaged PRs are allowed through.
		Use --all to block any PR whose base is not trunk.

		The trunk branch is detected from the git-spice
		navigation comment on the PR.
		Use --trunk to override this detection.

		Exit codes:
		  0  PR is safe to merge (base is trunk, or unmanaged)
		  1  PR should not be merged yet
	`)
}

func (cmd *ciMergeGuardCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo forge.Repository,
) error {
	changeID, err := cmd.resolveChangeID(repo)
	if err != nil {
		return err
	}

	change, err := repo.FindChangeByID(ctx, changeID)
	if err != nil {
		return fmt.Errorf("find change #%d: %w", cmd.Number, err)
	}

	trunk, managed, err := cmd.detectTrunk(ctx, log, repo, changeID)
	if err != nil {
		return err
	}

	return cmd.evaluate(log, change, trunk, managed)
}

func (cmd *ciMergeGuardCmd) resolveChangeID(
	repo forge.Repository,
) (forge.ChangeID, error) {
	raw := fmt.Appendf(nil, "%d", cmd.Number)
	id, err := repo.Forge().UnmarshalChangeID(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"construct change ID for #%d: %w", cmd.Number, err,
		)
	}
	return id, nil
}

// detectTrunk determines the trunk branch name and whether
// the PR is managed by git-spice.
// Returns (trunk, managed, error).
func (cmd *ciMergeGuardCmd) detectTrunk(
	ctx context.Context,
	log *silog.Logger,
	repo forge.Repository,
	changeID forge.ChangeID,
) (trunk string, managed bool, _ error) {
	if cmd.Trunk != "" {
		return cmd.Trunk, true, nil
	}

	trunk, managed = cmd.trunkFromNavComment(ctx, log, repo, changeID)
	if trunk != "" {
		return trunk, managed, nil
	}

	if !managed {
		return "", false, nil
	}

	return "", true, fmt.Errorf(
		"could not determine trunk for #%d: "+
			"use --trunk to specify it explicitly",
		cmd.Number,
	)
}

// trunkFromNavComment searches for a git-spice navigation comment
// on the given change and extracts the trunk branch name.
// Returns ("", false) if no navigation comment is found.
func (cmd *ciMergeGuardCmd) trunkFromNavComment(
	ctx context.Context,
	log *silog.Logger,
	repo forge.Repository,
	changeID forge.ChangeID,
) (trunk string, managed bool) {
	opts := &forge.ListChangeCommentsOptions{
		BodyMatchesAll: submit.NavCommentRegexes,
	}

	for comment, err := range repo.ListChangeComments(
		ctx, changeID, opts,
	) {
		if err != nil {
			log.Warn("Error listing comments", "error", err)
			return "", false
		}

		trunk := submit.ExtractTrunkFromComment(comment.Body)
		return trunk, true
	}

	return "", false
}

// evaluate decides whether the PR is safe to merge
// based on the change's base branch and the detected trunk.
func (cmd *ciMergeGuardCmd) evaluate(
	log *silog.Logger,
	change *forge.FindChangeItem,
	trunk string,
	managed bool,
) error {
	// Unmanaged PR: allow unless --all is set.
	if !managed {
		if cmd.All {
			return fmt.Errorf(
				"#%d: base %q is not trunk (unmanaged PR blocked by --all)",
				cmd.Number, change.BaseName,
			)
		}
		log.Infof("#%d: not managed by git-spice, allowing", cmd.Number)
		return nil
	}

	if change.BaseName == trunk {
		log.Infof("#%d: base is %q (trunk), safe to merge",
			cmd.Number, trunk)
		return nil
	}

	return fmt.Errorf(
		"#%d: base is %q, expected trunk %q. "+
			"Merge the downstack PR first or retarget to trunk",
		cmd.Number, change.BaseName, trunk,
	)
}
