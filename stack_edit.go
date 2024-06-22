package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type stackEditCmd struct {
	Editor string `env:"EDITOR" help:"Editor to use for editing the branches."`

	Name string `arg:"" optional:"" help:"Branch to edit from. Defaults to current branch." predictor:"trackedBranches"`
}

func (*stackEditCmd) Help() string {
	return text.Dedent(`
		Opens an editor to allow changing the order of branches
		in the current stack.
		Branches deleted from the list will not be modified.
	`)
}

func (cmd *stackEditCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Editor == "" {
		return errors.New("an editor is required: use --editor or set $EDITOR")
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	stack, err := svc.ListStackLinear(ctx, cmd.Name)
	if err != nil {
		var nonLinearErr *spice.NonLinearStackError
		if errors.As(err, &nonLinearErr) {
			// TODO: We could provide a prompt here to select a linear stack to edit from.
			log.Errorf("%v is part of a stack with a divergent upstack.", cmd.Name)
			log.Errorf("%v has multiple branches above it: %s", nonLinearErr.Branch, strings.Join(nonLinearErr.Aboves, ", "))
			log.Errorf("Check out one of those branches and try again.")
			return errors.New("current branch has ambiguous upstack")
		}
		return fmt.Errorf("list stack: %w", err)
	}

	// If current branch was trunk, it'll be at the bottom of the stack.
	if stack[0] == store.Trunk() {
		stack = stack[1:]
	}

	if len(stack) == 1 {
		log.Info("nothing to edit")
		return nil
	}

	_, err = svc.StackEdit(ctx, &spice.StackEditRequest{
		Editor: cmd.Editor,
		Stack:  stack,
	})
	if err != nil {
		if errors.Is(err, spice.ErrStackEditAborted) {
			log.Infof("stack edit aborted")
			return nil
		}

		// TODO: we can probably recover from the rebase operation
		// by saving the branch list somewhere,
		// and allowing it to be provided as input to the command.
		return fmt.Errorf("edit downstack: %w", err)
	}

	return (&branchCheckoutCmd{Name: cmd.Name}).Run(ctx, log, opts)
}
