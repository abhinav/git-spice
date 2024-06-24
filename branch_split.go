package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

// branchSplitCmd splits a branch into two or more branches
// along commit boundaries.
type branchSplitCmd struct {
	At     []branchSplit `placeholder:"COMMIT:NAME" help:"Commits to split the branch at."`
	Branch string        `placeholder:"NAME" help:"Branch to split commits of."`
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

func (cmd *branchSplitCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Branch == "" {
		cmd.Branch, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	if cmd.Branch == store.Trunk() {
		return fmt.Errorf("cannot split trunk")
	}

	branch, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", cmd.Branch, err)
	}

	// Commits are in oldest-to-newest order,
	// with last commit being branch head.
	branchCommits, err := repo.ListCommitsDetails(ctx,
		git.CommitRangeFrom(branch.Head).
			ExcludeFrom(branch.BaseHash).
			Reverse())
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}

	branchCommitHashes := make(map[git.Hash]struct{}, len(branchCommits))
	for _, commit := range branchCommits {
		branchCommitHashes[commit.Hash] = struct{}{}
	}

	// If len(cmd.At) == 0, run in interactive mode to build up cmd.At.
	if len(cmd.At) == 0 {
		if !opts.Prompt {
			return fmt.Errorf("use --at to split non-interactively: %w", errNoPrompt)
		}

		if len(branchCommits) < 2 {
			return errors.New("cannot split a branch with fewer than 2 commits")
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
		if err := ui.Run(selectWidget); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		selectedIdxes := selectWidget.Selected()
		if len(selectedIdxes) < 1 {
			return errors.New("no commits selected")
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

		if err := ui.NewForm(fields...).Run(); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		for i, split := range selected {
			cmd.At = append(cmd.At, branchSplit{
				Commit: split.Hash.String(),
				Name:   branchNames[i],
			})
		}
	}

	// Turn each commitish into a commit.
	commitHashes := make([]git.Hash, len(cmd.At))
	newTakenNames := make(map[string]int, len(cmd.At))
	for i, split := range cmd.At {
		// Interactive prompt verifies if the branch name is taken,
		// but we have to check again here.
		if repo.BranchExists(ctx, split.Name) {
			return fmt.Errorf("--at[%d]: branch already exists: %v", i, split.Name)
		}

		// Also prevent duplicate branch names speciifed as input.
		if otherIdx, ok := newTakenNames[split.Name]; ok {
			return fmt.Errorf("--at[%d]: branch name already taken by --at[%d]: %v", i, otherIdx, split.Name)
		}
		newTakenNames[split.Name] = i

		commitHash, err := repo.PeelToCommit(ctx, split.Commit)
		if err != nil {
			return fmt.Errorf("--at[%d]: resolve commit %q: %w", i, split.Commit, err)
		}

		// All commits must in base..head.
		// So you can't do 'split --at HEAD~10:newbranch'.
		if _, ok := branchCommitHashes[commitHash]; !ok {
			return fmt.Errorf("--at[%d]: %v (%v) is not in range %v..%v", i,
				split.Commit, commitHash, branch.Base, cmd.Branch)
		}
		commitHashes[i] = commitHash
	}

	// Split the branch. The commits are in oldest-to-newst order,
	// so we can just go through them in order.
	upserts := make([]state.UpsertRequest, 0, len(cmd.At)+1)
	base := branch.Base
	baseHash := branch.BaseHash
	for idx, split := range cmd.At {
		if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: split.Name,
			Head: commitHashes[idx].String(),
		}); err != nil {
			return fmt.Errorf("create branch %q: %w", split.Name, err)
		}

		upserts = append(upserts, state.UpsertRequest{
			Name:     split.Name,
			Base:     base,
			BaseHash: baseHash,
		})

		base = split.Name
		baseHash = commitHashes[idx]
	}

	// The last branch is the remainder of the original branch.
	upserts = append(upserts, state.UpsertRequest{
		Name:     cmd.Branch,
		Base:     base,
		BaseHash: baseHash,
	})

	if err := store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: upserts,
		Message: fmt.Sprintf("%v: split %d new branches", cmd.Branch, len(cmd.At)),
	}); err != nil {
		return fmt.Errorf("update store: %w", err)
	}

	return nil
}

type branchSplit struct {
	Commit string
	Name   string
}

func (b *branchSplit) Decode(ctx *kong.DecodeContext) error {
	var spec string
	if err := ctx.Scan.PopValueInto("at", &spec); err != nil {
		return err
	}

	idx := strings.LastIndex(spec, ":")
	switch {
	case idx == -1:
		return fmt.Errorf("expected COMMIT:NAME, got %q", spec)
	case len(spec[:idx]) == 0:
		return fmt.Errorf("part before : cannot be empty: %q", spec)
	case len(spec[idx+1:]) == 0:
		return fmt.Errorf("part after : cannot be empty: %q", spec)
	}

	b.Commit = spec[:idx]
	b.Name = spec[idx+1:]
	return nil
}
