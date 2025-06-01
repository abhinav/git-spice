package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
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

func (cmd *branchSplitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) (err error) {
	if cmd.Branch == "" {
		cmd.Branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	if cmd.Branch == store.Trunk() {
		return errors.New("cannot split trunk")
	}

	branch, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("lookup branch %q: %w", cmd.Branch, err)
	}

	// Commits are in oldest-to-newest order,
	// with last commit being branch head.
	branchCommits, err := sliceutil.CollectErr(repo.ListCommitsDetails(ctx,
		git.CommitRangeFrom(branch.Head).
			ExcludeFrom(branch.BaseHash).
			Reverse()))
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}

	branchCommitHashes := make(map[git.Hash]struct{}, len(branchCommits))
	for _, commit := range branchCommits {
		branchCommitHashes[commit.Hash] = struct{}{}
	}

	// If len(cmd.At) == 0, run in interactive mode to build up cmd.At.
	if len(cmd.At) == 0 {
		if !ui.Interactive(view) {
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
		if err := ui.Run(view, selectWidget); err != nil {
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

		if err := ui.Run(view, fields...); err != nil {
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
	newTakenNames := make(map[string]int, len(cmd.At)) // index into cmd.At
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

	// First we'll stage the state changes.
	// The commits are in oldest-to-newst order,
	// so we can just go through them in order.
	branchTx := store.BeginBranchTx()
	for idx, split := range cmd.At {
		base, baseHash := branch.Base, branch.BaseHash
		if idx > 0 {
			base, baseHash = cmd.At[idx-1].Name, commitHashes[idx-1]
		}

		if err := branchTx.Upsert(ctx, state.UpsertRequest{
			Name:     split.Name,
			Base:     base,
			BaseHash: baseHash,
		}); err != nil {
			return fmt.Errorf("add branch %v with base %v: %w", split.Name, base, err)
		}
		log.Debug("Updating tracked branch state",
			"branch", split.Name,
			"base", base+"@"+baseHash.String())
	}

	finalBase, finalBaseHash := branch.Base, branch.BaseHash
	if len(cmd.At) > 0 {
		finalBase, finalBaseHash = cmd.At[len(cmd.At)-1].Name, commitHashes[len(cmd.At)-1]
	}
	if err := branchTx.Upsert(ctx, state.UpsertRequest{
		Name:     cmd.Branch,
		Base:     finalBase,
		BaseHash: finalBaseHash,
	}); err != nil {
		return fmt.Errorf("update branch %v with base %v: %w", cmd.Branch, finalBase, err)
	}
	log.Debug("Updating tracked branch state",
		"branch", cmd.Branch,
		"base", finalBase+"@"+finalBaseHash.String())

	// If the branch being split had a Change associated with it,
	// ask the user which branch to associate the Change with.
	if branch.Change != nil && !ui.Interactive(view) {
		log.Info("Branch has an associated CR. Leaving it assigned to the original branch.",
			"cr", branch.Change.ChangeID())
	} else if branch.Change != nil {
		branchNames := make([]string, len(cmd.At)+1)
		for i, split := range cmd.At {
			branchNames[i] = split.Name
		}
		branchNames[len(branchNames)-1] = cmd.Branch

		// TODO:
		// use ll branch-style widget instead
		// showing the commits for each branch.

		var changeBranch string
		prompt := ui.NewSelect[string]().
			WithTitle(fmt.Sprintf("Assign CR %v to branch", branch.Change.ChangeID())).
			WithDescription("Branch being split has an open CR assigned to it.\n" +
				"Select which branch should take over the CR.").
			WithValue(&changeBranch).
			With(ui.ComparableOptions(cmd.Branch, branchNames...))
		if err := ui.Run(view, prompt); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}

		// The user selected a branch that is not the original branch
		// so update the Change metadata to reflect the new branch.
		if changeBranch != cmd.Branch {
			transfer, err := prepareChangeMetadataTransfer(
				ctx,
				log,
				forges,
				repo,
				store,
				cmd.Branch,
				changeBranch,
				branch.Change,
				branch.UpstreamBranch,
				branchTx,
			)
			if err != nil {
				return fmt.Errorf("transfer CR %v to %v: %w", branch.Change.ChangeID(), changeBranch, err)
			}

			// Perform the actual transfer only if the transaction succeeds.
			defer func() {
				if err == nil {
					transfer()
				}
			}()
		}
	}

	// State updates will probably succeed if we got here,
	// so make the branch changes in the repo.
	for idx, split := range cmd.At {
		if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: split.Name,
			Head: commitHashes[idx].String(),
		}); err != nil {
			return fmt.Errorf("create branch %q: %w", split.Name, err)
		}
	}

	if err := branchTx.Commit(ctx, fmt.Sprintf("%v: split %d new branches", cmd.Branch, len(cmd.At))); err != nil {
		return fmt.Errorf("update store: %w", err)
	}

	return nil
}

func prepareChangeMetadataTransfer(
	ctx context.Context,
	log *silog.Logger,
	forges *forge.Registry,
	repo *git.Repository,
	store *state.Store,
	fromBranch, toBranch string,
	meta forge.ChangeMetadata,
	upstreamBranch string,
	tx *state.BranchTx,
) (transfer func(), _ error) {
	forgeID := meta.ForgeID()
	f, ok := forges.Lookup(forgeID)
	if !ok {
		return nil, fmt.Errorf("unknown forge: %v", forgeID)
	}

	remote, err := store.Remote()
	if err != nil {
		return nil, fmt.Errorf("get remote: %w", err)
	}

	metaJSON, err := f.MarshalChangeMetadata(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal change metadata: %w", err)
	}

	// The original CR was pushed to this upstream branch name.
	// The new branch will inherit this upstream branch name.
	//
	// However, if this name matches the original branch name (which it usually does),
	// we'll want to warn the user that they should use a different name
	// when they push it upstream.
	toUpstreamBranch := cmp.Or(upstreamBranch, fromBranch)

	var empty string
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           fromBranch,
		ChangeMetadata: state.Null,
		UpstreamBranch: &empty,
	}); err != nil {
		return nil, fmt.Errorf("clear change metadata from %v: %w", fromBranch, err)
	}

	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:           toBranch,
		ChangeMetadata: metaJSON,
		ChangeForge:    forgeID,
		UpstreamBranch: &toUpstreamBranch,
	}); err != nil {
		return nil, fmt.Errorf("set change metadata on %v: %w", toBranch, err)
	}
	log.Debug("Transferring submitted CR metadata between tracked branches",
		"from", fromBranch, "to", toBranch)

	return func() {
		if err := repo.SetBranchUpstream(ctx, toBranch, remote+"/"+toUpstreamBranch); err != nil {
			log.Warnf("%v: Failed to set upstream branch %v: %v", toBranch, toUpstreamBranch, err)
		}

		if _, err := repo.BranchUpstream(ctx, fromBranch); err == nil {
			if err := repo.SetBranchUpstream(ctx, fromBranch, ""); err != nil {
				log.Warnf("%v: Failed to unset upstream branch %v: %v", fromBranch, upstreamBranch, err)
			}
		}

		log.Infof("%v: Upstream branch '%v' transferred to '%v'", fromBranch, toUpstreamBranch, toBranch)
		if toUpstreamBranch == fromBranch {
			pushCmd := fmt.Sprintf("git push -u %v %v:<new name>", remote, fromBranch)

			log.Warnf("%v: If you push this branch with 'git push' instead of 'gs branch submit',", fromBranch)
			log.Warnf("%v: remember to use a different upstream branch name with the command:\n\t%s", fromBranch, _highlightStyle.Render(pushCmd))
		}
	}, nil
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
