package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/fixup"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type commitFixupCmd struct {
	fixup.Options

	Commit string `arg:"" optional:"" help:"The commit to fixup"`
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

		This command requires at least Git 2.45.
	`)
}

type FixupHandler interface {
	FixupCommit(ctx context.Context, req *fixup.Request) error
}

func (cmd *commitFixupCmd) AfterApply(kctx *kong.Context) error {
	return kctx.BindToProvider(func(
		log *silog.Logger,
		repo *git.Repository,
		wt *git.Worktree,
		svc *spice.Service,
		restackHandler RestackHandler,
	) (FixupHandler, error) {
		return &fixup.Handler{
			Log:        log,
			Worktree:   wt,
			Repository: repo,
			Service:    svc,
			Restack:    restackHandler,
		}, nil
	})
}

func (cmd *commitFixupCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	svc *spice.Service,
	wt *git.Worktree,
	handler FixupHandler,
) (retErr error) {
	// TODO: Should we do a Git version check here?
	// git --version output is relatively stable.

	// Branch to check out after a successful fixup operation.
	// May be a detached HEAD.
	var checkoutDetached bool
	checkoutTarget, err := wt.CurrentBranch(ctx)
	if err != nil {
		if !errors.Is(err, git.ErrDetachedHead) {
			return fmt.Errorf("determine current branch: %w", err)
		}

		head, err := wt.Head(ctx)
		if err != nil {
			return fmt.Errorf("get HEAD commit: %w", err)
		}
		checkoutDetached = true
		checkoutTarget = head.String()
	}
	defer func() {
		// If the operation was successful,
		// check out the original branch again.
		if retErr != nil {
			return
		}

		checkoutFn := wt.Checkout
		if checkoutDetached {
			checkoutFn = wt.DetachHead
		}
		if err := checkoutFn(ctx, checkoutTarget); err != nil {
			log.Error("Could not check out original branch after fixup", "branch", checkoutTarget, "error", err)
			retErr = err
		}
	}()

	// There must be staged changes to commit.
	req := fixup.Request{
		Options: &cmd.Options,
		Commit:  "", // filled below
	}
	if cmd.Commit != "" {
		req.Commit, err = wt.PeelToCommit(ctx, cmd.Commit)
		if err != nil {
			return fmt.Errorf("resolve commit %q: %w", cmd.Commit, err)
		}
		if string(req.Commit) != cmd.Commit {
			log.Debug("Fixup commit resolved", "commit", req.Commit)
		}
	} else {
		if !ui.Interactive(view) {
			return fmt.Errorf("no commit specified: %w", errNoPrompt)
		}

		req.Branch, req.Commit, err = cmd.commitPrompt(ctx, log, view, repo, wt, svc)
		if err != nil {
			return fmt.Errorf("prompt for commit: %w", err)
		}
	}
	must.NotBeBlankf(req.Commit, "commit hash not specified, nor set in prompt")
	return handler.FixupCommit(ctx, &req)
}

func (cmd *commitFixupCmd) commitPrompt(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	svc *spice.Service,
) (string, git.Hash, error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			return "", "", errors.New("no commit specified and HEAD is detached; cannot prompt")
		}

		return "", "", fmt.Errorf("determine current branch: %w", err)
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("load branch graph: %w", err)
	}

	var (
		mu           sync.Mutex
		wg           sync.WaitGroup
		totalCommits int
	)
	var branches []widget.CommitPickBranch
	shortToLongHash := make(map[git.Hash]git.Hash)
	longHashToBranch := make(map[git.Hash]string)

	branchc := make(chan string)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for name := range branchc {
				// TODO:
				// Awful lot of duplication here
				// with how list.Handler works.
				// Might want to re-use that here somehow.
				//
				// Or push commit range listing into graph.
				b, ok := graph.Lookup(name)
				if !ok {
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
		})
	}

	for name := range graph.Downstack(currentBranch) {
		if name == graph.Trunk() {
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
