package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/split"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type branchSplitCmd struct {
	split.Options

	Branch string `placeholder:"NAME" help:"Branch to split commits of."`
}

func (*branchSplitCmd) Help() string {
	return text.Dedent(`
		Splits the current branch into two or more branches at specific
		commits, inserting the new branches into the stack
		at the positions of the commits.
		Use the --branch flag to specify a different branch to split.

		By default, the command will prompt for commits to introduce
		splits at.
		Supply the --at flag one or more times to split a branch
		without a prompt.

			--at COMMIT:NAME

		Where COMMIT resolves to a commit per gitrevisions(7),
		and NAME is the name of the new branch.
		For example:

			# split at a specific commit
			gs branch split --at 1234567:newbranch

			# split at the previous commit
			gs branch split --at HEAD^:newbranch
	`)
}

// SplitHandler is a subset of split.Handler.
type SplitHandler interface {
	SplitBranch(ctx context.Context, req *split.BranchRequest) error
}

var _ SplitHandler = (*split.Handler)(nil)

func (cmd *branchSplitCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		branch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = branch
	}

	return nil
}

func (cmd *branchSplitCmd) Run(
	ctx context.Context,
	view ui.View,
	repo *git.Repository,
	splitHandler SplitHandler,
) error {
	return splitHandler.SplitBranch(ctx, &split.BranchRequest{
		Branch:  cmd.Branch,
		Options: &cmd.Options,
		SelectCommits: func(ctx context.Context, branchCommits []git.CommitDetail) ([]split.Point, error) {
			if !ui.Interactive(view) {
				return nil, fmt.Errorf("use --at to split non-interactively: %w", ui.ErrPrompt)
			}

			if len(branchCommits) < 2 {
				return nil, errors.New("cannot split a branch with fewer than 2 commits")
			}

			commits := make([]widget.CommitSummary, len(branchCommits))
			for i, commit := range branchCommits {
				commits[i] = widget.CommitSummary{
					ShortHash:  commit.ShortHash,
					Subject:    commit.Subject,
					AuthorDate: commit.AuthorDate,
				}
			}

			selectWidget := widget.NewBranchSplit().
				WithTitle("Select commits").
				WithHEAD(cmd.Branch).
				WithDescription("Select commits to split the branch at").
				WithCommits(commits...)
			if err := ui.Run(view, selectWidget); err != nil {
				return nil, fmt.Errorf("prompt: %w", err)
			}

			selectedIdxes := selectWidget.Selected()
			if len(selectedIdxes) < 1 {
				return nil, errors.New("no commits selected")
			}

			selected := make([]git.CommitDetail, len(selectedIdxes))
			for i, idx := range selectedIdxes {
				selected[i] = branchCommits[idx]
			}

			fields := make([]ui.Field, len(selected))
			branchNames := make([]string, len(selected))
			for i, commit := range selected {
				var desc strings.Builder
				desc.WriteString("  □ ")
				(&widget.CommitSummary{
					ShortHash:  commit.ShortHash,
					Subject:    commit.Subject,
					AuthorDate: commit.AuthorDate,
				}).Render(&desc, widget.DefaultCommitSummaryStyle)

				input := ui.NewInput().
					WithTitle("Branch name").
					WithDescription(desc.String()).
					WithValidate(func(value string) error {
						if strings.TrimSpace(value) == "" {
							return errors.New("branch name cannot be empty")
						}
						if repo.BranchExists(ctx, value) {
							return fmt.Errorf("branch name already taken: %v", value)
						}
						return nil
					}).
					WithValue(&branchNames[i])
				fields[i] = input
			}

			if err := ui.Run(view, fields...); err != nil {
				return nil, fmt.Errorf("prompt: %w", err)
			}

			splits := make([]split.Point, 0, len(selected))
			for i, commit := range selected {
				splits = append(splits, split.Point{
					Commit: commit.Hash.String(),
					Name:   branchNames[i],
				})
			}

			return splits, nil
		},
	})
}
