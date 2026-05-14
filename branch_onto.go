package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/onto"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchOntoCmd struct {
	BranchPromptConfig

	Branch  string            `help:"Branch to move" placeholder:"NAME" predictor:"trackedBranches"`
	Restack spice.RestackMode `default:"none" config:"branchOnto.restack" enum:"none,aboves,upstack" help:"How to restack branches above the moved branch. One of 'none', 'aboves', and 'upstack'."`
	Onto    string            `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*branchOntoCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Commits of the current branch
		are transplanted onto another branch
		while leaving the rest of the stack intact.
		That is, branches above the current branch
		are retargeted onto its original base,
		and then the current branch is moved onto the new base.

		Use --restack to rebase those branches and their upstacks
		immediately after retargeting.

		A prompt will allow selecting the new base for the branch.
		Provide an argument to skip the prompt.
		Use the --branch flag to target a different branch for the move.

		For example, given the following stack with B checked out,
		running '%[1]s branch onto main' will move B onto main
		and leave C on top of A.

			       %[1]s branch onto main

			    ┌── C               ┌── B ◀
			  ┌─┴ B ◀               │ ┌── C
			┌─┴ A                   ├─┴ A
			trunk                   trunk

		Use '%[1]s upstack onto' to also move the upstack branches.
	`, name))
}

// OntoHandler coordinates branch and upstack base changes.
type OntoHandler interface {
	BranchOnto(context.Context, *onto.BranchRequest) error
	UpstackOnto(context.Context, *onto.UpstackRequest) error
}

var _ OntoHandler = (*onto.Handler)(nil)

func (cmd *branchOntoCmd) AfterApply(
	ctx context.Context,
	view ui.View,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	branchPrompt *branchPrompter,
) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if cmd.Branch == store.Trunk() {
		return errors.New("cannot move trunk")
	}

	if cmd.Onto == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		// TODO: cache between AfterApply and Run?
		branch, err := svc.LookupBranch(ctx, cmd.Branch)
		if err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return fmt.Errorf("branch not tracked: %s", cmd.Branch)
			}
			return fmt.Errorf("get branch: %w", err)
		}

		cmd.Onto, err = branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name == cmd.Branch
			},
			TrackedOnly: true,
			Worktree:    wt.RootDir(),
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving %s onto another branch", cmd.Branch),
		})
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	return nil
}

func (cmd *branchOntoCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	handler OntoHandler,
	submoduleApplier SubmoduleApplier,
) error {
	// Snapshot parent HEAD before the checkout so a submodule apply
	// failure rolls back cleanly. branch onto bypasses checkout.Handler
	// (its VerifyRestacked path is not appropriate post-BranchOnto),
	// so we apply submodule associations directly after the handler runs.
	parentSnap, snapErr := wt.SnapshotHead(ctx)
	if snapErr != nil {
		log.Warn("Could not snapshot HEAD before checkout; "+
			"submodule rollback disabled",
			"error", snapErr)
	}

	if err := handler.BranchOnto(ctx, &onto.BranchRequest{
		Branch:          cmd.Branch,
		Onto:            cmd.Onto,
		Restack:         cmd.Restack,
		ContinueCommand: cmd.continueCommand(),
	}); err != nil {
		return err
	}

	if err := submoduleApplier.ApplyAssociations(ctx, cmd.Branch); err != nil {
		if parentSnap != nil {
			if rerr := wt.RestoreHead(ctx, parentSnap); rerr != nil {
				log.Warn("Parent rollback failed after submodule conflict",
					"target", parentSnap.Hash,
					"error", rerr)
			}
		}
		return fmt.Errorf("apply submodules: %w", err)
	}

	return nil
}

func (cmd *branchOntoCmd) continueCommand() []string {
	contCmd := []string{"branch", "onto", "--branch", cmd.Branch}
	if !cmd.Restack.Includes(spice.RestackNone) {
		contCmd = append(contCmd, "--restack="+cmd.Restack.String())
	}
	return append(contCmd, cmd.Onto)
}
