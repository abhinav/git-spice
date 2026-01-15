package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/fixup"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/commit"
	"go.abhg.dev/gs/internal/ui/widget"
)

type commitFixupCmd struct {
	fixup.Options

	Commit string `arg:"" optional:"" help:"The commit to fixup. Must be reachable from the HEAD commit."`
}

func (cmd *commitFixupCmd) Help() string {
	return text.Dedent(`
		Apply staged uncommited changes to another commit
		down the stack, and restack the rest of the stack on top of it.

		If a commit is not specified, a prompt is shown to select one.
		If the commit is specified,
		it must be reachable from the current commit,
		(i.e. it must be down the stack).

		If it's not possible to apply the staged changes to the commit
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
	autostashHandler AutostashHandler,
) (retErr error) {
	// TODO: Should we do a Git version check here?
	// git --version output is relatively stable.

	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			// TODO: To support fixup from detached HEAD,
			// we'll need to know what the rebased commit HEAD is.
			return errors.New("HEAD is detached; cannot fixup commit")
		}
		return fmt.Errorf("determine current branch: %w", err)
	}

	cleanup, err := autostashHandler.BeginAutostash(ctx, &autostash.Options{
		Message:   "git-spice: autostash before commit fixup",
		ResetMode: autostash.ResetWorktree,
		Branch:    currentBranch,
	})
	if err != nil {
		return err
	}

	defer func() {
		if retErr == nil {
			if err := wt.CheckoutBranch(ctx, currentBranch); err != nil {
				retErr = fmt.Errorf("restore original branch %q: %w", currentBranch, err)
			}
		}
		cleanup(&retErr)
	}()

	var (
		commitHash   git.Hash
		commitBranch string
	)
	if cmd.Commit != "" {
		var err error
		commitHash, err = wt.PeelToCommit(ctx, cmd.Commit)
		if err != nil {
			return fmt.Errorf("not a commit: %q: %w", cmd.Commit, err)
		}
		if string(commitHash) != cmd.Commit {
			log.Debugf("Commit resolved: %v", commitHash)
		}
	} else {
		if !ui.Interactive(view) {
			return fmt.Errorf("no commit specified: %w", errNoPrompt)
		}

		var err error
		commitBranch, commitHash, err = cmd.commitPrompt(ctx, log, view, repo, svc, currentBranch)
		if err != nil {
			return fmt.Errorf("prompt for commit: %w", err)
		}
	}
	must.NotBeBlankf(commitHash, "commit hash not specified, nor set in prompt")
	req := &fixup.Request{
		Options:      &cmd.Options,
		TargetHash:   commitHash,
		TargetBranch: commitBranch,
		HeadBranch:   currentBranch,
	}
	if err := handler.FixupCommit(ctx, req); err != nil {
		// If the fixup fails because of a rebase conflict,
		// after the conflict is resolved and other operations done
		// (e.g. restack), we want to return to the original branch.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Branch:  currentBranch,
			Command: []string{"branch", "checkout", currentBranch},
			Message: fmt.Sprintf("fixup commit %s", commitHash),
		})
	}

	return nil
}

func (cmd *commitFixupCmd) commitPrompt(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	svc *spice.Service,
	currentBranch string,
) (string, git.Hash, error) {
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

				commitSummaries := make([]commit.Summary, len(commits))

				mu.Lock()
				for i, c := range commits {
					commitSummaries[i] = commit.Summary{
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
