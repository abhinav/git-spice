package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type commitFixupCmd struct {
	Commit     string `arg:"" optional:"" help:"The commit to fixup"`
	Autosquash bool   `default:"true" negatable:"" config:"commitFixup.autosquash" help:"Automatically squash the change into the target commit"`
	Edit       bool   `default:"true" negatable:"" config:"commitFixup.edit" help:"Edit the commit message after autosquashing"`
}

func (cmd *commitFixupCmd) Help() string {
	return text.Dedent(`
		Apply staged uncommited changes to another commit
		down the stack, and restack the rest of the stack on top of it.

		If a commit is not specified, a prompt is shown to select one.
		If the commit is specified, it must be reachable from the current commit,
		(i.e. it must be down the stack).

		If it's not possible to apply the changes to the commit
		without causing a conflict, the command will fail.

		By default, the commit is squashed into the target commit
		(equivalent to amending that commit).
		Opt-out of this with --no-autosquash.

		If the commit is autosquashed,
		an editor is opened to edit the commit message.
		Opt-out of this with --no-edit.

		This command requires at least Git 2.40.
	`)
}

func (cmd *commitFixupCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) (err error) {
	// TODO: git version check

	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			return errors.New("commit fixup is not supported in detached head state")
		}

		return fmt.Errorf("determine current branch: %w", err)
	}

	head, err := wt.Head(ctx)
	if err != nil {
		return fmt.Errorf("determine HEAD: %w", err)
	}

	// There must be staged changes to commit.
	if diff, err := wt.DiffIndex(ctx, head.String()); err != nil {
		return fmt.Errorf("diff index: %w", err)
	} else if len(diff) == 0 {
		return errors.New("no changes staged for commit")
	}

	// TODO: need to know the branch for the target commit
	targetBranch := branch
	targetCommit := git.Hash(cmd.Commit)
	if targetCommit != "" {
		head, err := wt.Head(ctx)
		if err != nil {
			return fmt.Errorf("determine HEAD: %w", err)
		}

		if !repo.IsAncestor(ctx, targetCommit, head) {
			log.Errorf("commit is not reachable from HEAD: %s", cmd.Commit)
			return errors.New("fixup commit must be reachable from HEAD")
		}

		panic("figure out downstack branch from commit")
	} else {
		if !ui.Interactive(view) {
			return fmt.Errorf("no commit specified: %w", errNoPrompt)
		}

		targetBranch, targetCommit, err = cmd.commitPrompt(ctx, log, view, repo, store, svc, branch)
		if err != nil {
			return fmt.Errorf("prompt for commit: %w", err)
		}
	}
	must.NotBeBlankf(targetCommit, "commit hash not specified, nor set in prompt")

	plannedTree, err := wt.WriteIndexTree(ctx)
	if err != nil {
		return fmt.Errorf("write staged changes to tree: %w", err)
	}

	mergedTree, err := repo.MergeTree(ctx, git.MergeTreeRequest{
		Branch1:   plannedTree.String(),
		Branch2:   targetCommit.String(),
		MergeBase: head.String(),
	})
	if err != nil {
		// TODO: figure out error message
		log.Error("Merging staged changes into target commit failed", "error", err)
		return errors.New("staged changes cannot be applied to the target commit")
	}

	newCommit, err := repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:    mergedTree,
		Parents: []git.Hash{targetCommit},
		Message: fmt.Sprintf("fixup! %s", targetCommit),
		// TODO: edit?
	})
	if err != nil {
		return fmt.Errorf("commit staged changes to target commit: %w", err)
	}

	// TODO: for now we'll do this with a rebase. Figure out better.
	if err := wt.Rebase(ctx, git.RebaseRequest{
		Branch:    targetBranch,
		Onto:      newCommit.String(),
		Upstream:  targetCommit.String(),
		Autostash: true,
		// TODO: Autosquash
	}); err != nil {
		return fmt.Errorf("rebase onto new commit: %w", err)
	}

	// TODO: check out original branch after restack

	return (&upstackRestackCmd{
		Branch:    targetBranch,
		SkipStart: true,
	}).Run(ctx, log, wt, store, svc)
}

func (cmd *commitFixupCmd) commitPrompt(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
	currentBranch string,
) (string, git.Hash, error) {
	downstack, err := svc.ListDownstack(ctx, currentBranch)
	if err != nil {
		return "", "", fmt.Errorf("list downstack of %v: %w", currentBranch, err)
	}

	var (
		mu           sync.Mutex
		wg           sync.WaitGroup
		totalCommits int
	)
	branches := make([]widget.CommitPickBranch, 0, len(downstack))
	shortToLongHash := make(map[git.Hash]git.Hash)
	longHashToBranch := make(map[git.Hash]string)

	branchc := make(chan string)
	numWorkers := min(runtime.GOMAXPROCS(0), len(downstack))
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range branchc {
				b, err := svc.LookupBranch(ctx, name)
				if err != nil {
					log.Warn("Could not look up branch. Skipping.",
						"branch", name, "error", err)
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

				if len(commits) == 0 {
					continue
				}

				commitSummaries := make([]widget.CommitSummary, len(commits))

				mu.Lock()
				for i, c := range commits {
					commitSummaries[i] = widget.CommitSummary{
						ShortHash:  c.ShortHash,
						Subject:    c.Subject,
						AuthorDate: c.AuthorDate,
					}
					shortToLongHash[c.ShortHash] = c.Hash
					longHashToBranch[c.Hash] = name
				}
				branches = append(branches, widget.CommitPickBranch{
					Branch:  name,
					Base:    b.Base,
					Commits: commitSummaries,
				})
				totalCommits += len(commitSummaries)
				mu.Unlock()

			}
		}()
	}

	for _, name := range downstack {
		if name == store.Trunk() {
			continue
		}

		branchc <- name
	}
	close(branchc)
	wg.Wait()

	if totalCommits == 0 {
		return "", "", fmt.Errorf("downstack of %v does not have any commits to cherry-pick", currentBranch)
	}

	var selected git.Hash
	prompt := widget.NewCommitPick().
		WithTitle("Pick a commit").
		WithDescription("Staged changes will be applied to this commit.").
		WithBranches(branches...).
		WithValue(&selected)
	if err := ui.Run(view, prompt); err != nil {
		return "", "", err
	}

	if long, ok := shortToLongHash[selected]; ok {
		// This will always be true but it doesn't hurt
		// to be defensive here.
		selected = long
	}
	branch, ok := longHashToBranch[selected]
	must.Bef(ok, "selected commit %s has no branch associated with it", selected)
	return branch, selected, nil
}
