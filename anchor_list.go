package main

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type anchorListCmd struct{}

func (*anchorListCmd) Help() string {
	return text.Dedent(`
		Lists all worktrees associated with the repository.
		For each worktree, shows the checked-out branch
		and any tracked branches in its stack.
	`)
}

func (*anchorListCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	svc *spice.Service,
	wt *git.Worktree,
) error {
	branchGraph, err := svc.BranchGraph(ctx,
		&spice.BranchGraphOptions{
			IncludeWorktrees: true,
		},
	)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}

	trunk := branchGraph.Trunk()
	currentWT := wt.RootDir()

	for item, err := range repo.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}

		if item.Bare {
			continue
		}

		branchName := item.Branch
		if branchName == "" {
			branchName = "(detached)"
		}

		// Build stack description for the checked-out branch.
		var stackDesc string
		if branchName != "" && branchName != trunk {
			if _, ok := branchGraph.Lookup(branchName); ok {
				var parts []string
				for b := range branchGraph.Stack(branchName) {
					parts = append(parts, b)
				}
				if len(parts) > 1 {
					stackDesc = strings.Join(parts, " → ")
				}
			}
		}

		// Build the display line.
		marker := " "
		if item.Path == currentWT {
			marker = "*"
		}

		line := fmt.Sprintf(
			"%s %s\t%s",
			marker, item.Path, branchName,
		)

		if branchName == trunk {
			line += " (trunk)"
		}

		if stackDesc != "" {
			line += "\t[" + stackDesc + "]"
		}

		log.Infof("%s", line)
	}

	return nil
}
