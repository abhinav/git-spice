package main

import (
	"cmp"
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type commitPickCmd struct {
	Commit string `arg:"" optional:"" help:"Commit to cherry-pick"`
	// TODO: Support multiple commits similarly to git cherry-pick.

	Edit bool   `default:"false" negatable:"" config:"commitPick.edit" help:"Whether to open an editor to edit the commit message."`
	From string `placeholder:"NAME" predictor:"trackedBranches" help:"Branch whose upstack commits will be considered."`
}

func (*commitPickCmd) Help() string {
	return text.Dedent(`
		Apply the changes introduced by a commit to the current branch
		and restack the upstack branches.

		If a commit is not specified, a prompt will allow picking
		from commits of upstack branches of the current branch.
		Use the --from option to pick a commit from a different branch
		or its upstack.

		By default, commit messages for cherry-picked commits will be used verbatim.
		Supply --edit to open an editor and change the commit message,
		or set the spice.commitPick.edit configuration option to true
		to always open an editor for cherry picks.
	`)
}

func (cmd *commitPickCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) (err error) {
	var commit git.Hash
	if cmd.Commit == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("no commit specified: %w", errNoPrompt)
		}

		commit, err = cmd.commitPrompt(ctx, log, view, repo, wt, store, svc)
		if err != nil {
			return fmt.Errorf("prompt for commit: %w", err)
		}
	} else {
		commit, err = repo.PeelToCommit(ctx, cmd.Commit)
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}
	}

	log.Debugf("Cherry-picking: %v", commit)
	err = repo.CherryPick(ctx, git.CherryPickRequest{
		Commits: []git.Hash{commit},
		Edit:    cmd.Edit,
		// If you selected an empty commit,
		// you probably want to retain that.
		// This still won't allow for no-op cherry-picks.
		AllowEmpty: true,
	})
	if err != nil {
		return fmt.Errorf("cherry-pick: %w", err)
	}

	// TODO: cherry-pick the commit
	// TODO: handle --continue/--abort
	// TODO: upstack restack
	return nil
}

func (cmd *commitPickCmd) commitPrompt(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) (git.Hash, error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		// TODO: allow for cherry-pick onto non-branch HEAD.
		return "", fmt.Errorf("determine current branch: %w", err)
	}
	cmd.From = cmp.Or(cmd.From, currentBranch)

	upstack, err := svc.ListUpstack(ctx, cmd.From)
	if err != nil {
		return "", fmt.Errorf("list upstack branches: %w", err)
	}

	var totalCommits int
	branches := make([]widget.CommitPickBranch, 0, len(upstack))
	shortToLongHash := make(map[git.Hash]git.Hash)
	for _, name := range upstack {
		if name == store.Trunk() {
			continue
		}

		// TODO: build commit list for each branch concurrently
		b, err := svc.LookupBranch(ctx, name)
		if err != nil {
			log.Warn("Could not look up branch. Skipping.",
				"branch", name, "error", err)
			continue
		}

		// If doing a --from=$other,
		// where $other is downstack from current,
		// we don't want to list commits for current branch,
		// so add an empty entry for it.
		if name == currentBranch {
			// Don't list the current branch's commits.
			branches = append(branches, widget.CommitPickBranch{
				Branch: name,
				Base:   b.Base,
			})
			continue
		}

		commits, err := sliceutil.CollectErr(repo.ListCommitsDetails(ctx,
			git.CommitRangeFrom(b.Head).
				ExcludeFrom(b.BaseHash).
				FirstParent()))
		if err != nil {
			log.Warn("Could not list commits for branch. Skipping.",
				"branch", name, "error", err)
			continue
		}

		commitSummaries := make([]widget.CommitSummary, len(commits))
		for i, c := range commits {
			commitSummaries[i] = widget.CommitSummary{
				ShortHash:  c.ShortHash,
				Subject:    c.Subject,
				AuthorDate: c.AuthorDate,
			}
			shortToLongHash[c.ShortHash] = c.Hash
		}

		branches = append(branches, widget.CommitPickBranch{
			Branch:  name,
			Base:    b.Base,
			Commits: commitSummaries,
		})
		totalCommits += len(commitSummaries)
	}

	if totalCommits == 0 {
		log.Warn("Please provide a commit hash to cherry pick from.")
		return "", fmt.Errorf("upstack of %v does not have any commits to cherry-pick", cmd.From)
	}

	msg := fmt.Sprintf("Selected commit will be cherry-picked into %v", currentBranch)
	var selected git.Hash
	prompt := widget.NewCommitPick().
		WithTitle("Pick a commit").
		WithDescription(msg).
		WithBranches(branches...).
		WithValue(&selected)
	if err := ui.Run(view, prompt); err != nil {
		return "", err
	}

	if long, ok := shortToLongHash[selected]; ok {
		// This will always be true but it doesn't hurt
		// to be defensive here.
		selected = long
	}
	return selected, nil
}
