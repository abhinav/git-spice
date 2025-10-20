package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
		Splits the current branch into two or more branches
		at specific commits,
		inserting the new branches into the stack
		at the positions of the commits.
		Use the --branch flag to specify a different branch to split.

		The command will prompt for commits to introduce splits at.
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

		If the original branch is assigned to one of the splits,
		it is required to provide a new name for the commit at HEAD.
		Fo example, if we have branch A with three commits:

			┌─ A
			│  abcdef1 Commit 3 (HEAD)
			│  bcdef12 Commit 2
			│  cdef123 Commit 1
			trunk

		A split at commit 2 using the branch name "A"
		would require a new name to be provided for commit 3.
	`)
}

// SplitHandler is a subset of split.Handler.
type SplitHandler interface {
	SplitBranch(ctx context.Context, req *split.BranchRequest) (*split.BranchResult, error)
}

var _ SplitHandler = (*split.Handler)(nil)

func (cmd *branchSplitCmd) Run(
	ctx context.Context,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	splitHandler SplitHandler,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	if cmd.Branch == "" {
		cmd.Branch = currentBranch
	}

	result, err := splitHandler.SplitBranch(ctx, &split.BranchRequest{
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

			// branchNames[i] is the name for selected[i]
			branchNames := make([]string, len(selected))

			branchNameWidget := func(desc string, value *string) ui.Field {
				return ui.NewInput().
					WithTitle("Branch name").
					WithDescription(desc).
					WithValidate(func(value string) error {
						value = strings.TrimSpace(value)
						if value == "" {
							return errors.New("branch name cannot be empty")
						}
						if idx := slices.Index(branchNames, value); idx >= 0 {
							return fmt.Errorf("name already used for commit: %v", selected[idx].ShortHash)
						}
						if value != cmd.Branch && repo.BranchExists(ctx, value) {
							return fmt.Errorf("branch name already taken: %v", value)
						}
						return nil
					}).
					WithValue(value)
			}

			fields := make([]ui.Field, 0, len(selected)+1) // +1 for deferred HEAD field
			for i, commit := range selected {
				desc := cmd.commitDescription(commit, false /* head */)
				input := branchNameWidget(desc, &branchNames[i])
				fields = append(fields, input)
			}

			// New name for the commit at HEAD
			// if the original branch is being moved.
			// This is a deferred field so it will be added
			// only if the original name is reused in the list
			// above.
			var headBranchName string
			headCommit := branchCommits[len(branchCommits)-1]
			fields = append(fields, ui.Defer(func() ui.Field {
				if !slices.Contains(branchNames, cmd.Branch) {
					// Original name not reused, no need to prompt
					return nil
				}

				desc := cmd.commitDescription(headCommit, true /* head */) +
					" [" + cmd.Branch + "]"
				return branchNameWidget(desc, &headBranchName)
			}))

			if err := ui.Run(view, fields...); err != nil {
				return nil, fmt.Errorf("prompt: %w", err)
			}

			splits := make([]split.Point, 0, len(selected))
			for i, commit := range selected {
				splits = append(splits, split.Point{
					Commit: commit.Hash.String(),
					Name:   strings.TrimSpace(branchNames[i]),
				})
			}

			// If a new name was selected for the HEAD commit,
			// add that as a split too.
			if headBranchName != "" {
				splits = append(splits, split.Point{
					Commit: headCommit.Hash.String(),
					Name:   strings.TrimSpace(headBranchName),
				})
			}

			return splits, nil
		},
	})
	if err != nil {
		return err
	}

	// If post-split, the topmost branch is not the current branch,
	// (because the current branch was moved into a downstream position),
	// checkout the new topmost branch.
	if result.Top != currentBranch {
		if err := wt.CheckoutBranch(ctx, result.Top); err != nil {
			return fmt.Errorf("checkout branch %q: %w", result.Top, err)
		}
	}

	return nil
}

func (cmd *branchSplitCmd) commitDescription(commit git.CommitDetail, head bool) string {
	var desc strings.Builder
	if head {
		desc.WriteString("  ■ ")
	} else {
		desc.WriteString("  □ ")
	}
	(&widget.CommitSummary{
		ShortHash:  commit.ShortHash,
		Subject:    commit.Subject,
		AuthorDate: commit.AuthorDate,
	}).Render(&desc, widget.DefaultCommitSummaryStyle)

	return desc.String()
}
