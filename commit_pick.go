package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/cherrypick"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
	"go.abhg.dev/gs/internal/ui/widget"
)

type commitPickCmd struct {
	cherrypick.Options

	Commit string `arg:"" optional:"" help:"Commit to cherry-pick"`
	From   string `placeholder:"NAME" predictor:"trackedBranches" help:"Branch whose upstack commits will be considered."`
}

func (*commitPickCmd) Help() string {
	return text.Dedent(`
		Apply the changes introduced by a commit to the current branch
		and restack the upstack branches.

		If a commit is not specified, a prompt will allow picking
		from commits of upstack branches of the current branch.
		Use the --from option to pick a commit from a different branch
		or its upstack.

		If it's not possible to cherry-pick the requested commit
		without causing a conflict, the command will fail.
		If the requested commit is a merge commit,
		the command will fail.

		This command requires at least Git 2.45.
	`)
}

type CherryPickHandler interface {
	CherryPickCommit(ctx context.Context, req *cherrypick.Request) error
}

func (cmd *commitPickCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	svc *spice.Service,
	cherryPickHandler CherryPickHandler,
) (err error) {
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			return errors.New("cannot cherry-pick onto detached HEAD")
		}
		return fmt.Errorf("determine current branch: %w", err)
	}
	cmd.From = cmp.Or(cmd.From, branch)

	var commit git.Hash
	if cmd.Commit == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("no commit specified: %w", errNoPrompt)
		}

		commit, err = cmd.commitPrompt(ctx, log, view, repo, svc, branch)
		if err != nil {
			return fmt.Errorf("prompt for commit: %w", err)
		}
	} else {
		commit, err = repo.PeelToCommit(ctx, cmd.Commit)
		if err != nil {
			return fmt.Errorf("peel to commit: %w", err)
		}
	}

	if branch == svc.Trunk() {
		if !ui.Interactive(view) {
			log.Warnf("You are about to cherry-pick commit %v on the trunk branch (%v).", commit.Short(), svc.Trunk())
		} else {
			var pickOnTrunk bool
			prompt := ui.NewList[bool]().
				WithTitle("Do you want to cherry-pick on trunk?").
				WithDescription(fmt.Sprintf("You are about to cherry-pick commit %v on the trunk branch (%v). "+
					"This is usually not what you want to do.", commit.Short(), svc.Trunk())).
				WithItems(
					ui.ListItem[bool]{
						Title: "Yes",
						Description: func(bool) string {
							return fmt.Sprintf("Cherry-pick commit %v on trunk", commit.Short())
						},
						Value: true,
					},
					ui.ListItem[bool]{
						Title: "No",
						Description: func(bool) string {
							return "Abort the operation"
						},
						Value: false,
					},
				).
				WithValue(&pickOnTrunk)

			if err := ui.Run(view, prompt); err != nil {
				return fmt.Errorf("prompt: %w", err)
			}

			if !pickOnTrunk {
				return errors.New("operation aborted")
			}
		}
	}

	log.Debugf("Cherry-picking: %v", commit)
	return cherryPickHandler.CherryPickCommit(ctx, &cherrypick.Request{
		Commit:  commit,
		Branch:  branch,
		Options: &cmd.Options,
	})
}

func (cmd *commitPickCmd) commitPrompt(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	svc *spice.Service,
	currentBranch string,
) (git.Hash, error) {
	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("load branch graph: %w", err)
	}

	var totalCommits int
	shortToLongHash := make(map[git.Hash]git.Hash)
	var branches []widget.CommitPickBranch
	for name := range graph.Upstack(cmd.From) {
		if name == graph.Trunk() {
			continue
		}

		// TODO: build commit list for each branch concurrently
		b, ok := graph.Lookup(name)
		if !ok {
			continue // not really possible once past trunk
		}

		// If doing a --from=$other,
		// where $other is downstack from current,
		// we don't want to list commits for current branch,
		// so add an empty entry for it.
		if name == currentBranch {
			branches = append(branches, widget.CommitPickBranch{
				Branch: name,
				Base:   b.Base,
			})
			continue
		}

		// TODO: parallel fetching?
		commits, err := sliceutil.CollectErr(repo.ListCommitsDetails(ctx,
			git.CommitRangeFrom(b.Head).
				ExcludeFrom(b.BaseHash).
				FirstParent()))
		if err != nil {
			log.Warn("Could not list commits for branch. Skipping.",
				"branch", name, "error", err)
			continue
		}

		commitSummaries := make([]commit.Summary, len(commits))
		for i, c := range commits {
			commitSummaries[i] = commit.Summary{
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
