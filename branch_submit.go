package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/git"
	"golang.org/x/oauth2"
)

type branchSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
}

func (cmd *branchSubmitCmd) Run(ctx context.Context, log *log.Logger, tokenSource oauth2.TokenSource) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	commitHash, err := repo.PeelToCommit(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	branch, err := store.Lookup(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("lookup branch: %w", err)
	}

	ghrepo := store.GitHubRepo()

	gh := github.NewClient(oauth2.NewClient(ctx, tokenSource))
	pulls, _, err := gh.PullRequests.List(ctx, ghrepo.Owner, ghrepo.Name, &github.PullRequestListOptions{
		State: "open",
		Head:  ghrepo.Owner + ":" + currentBranch,
		// Don't filter by base -- we may need to update it.
	})
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}

	switch len(pulls) {
	case 0:
		if cmd.DryRun {
			log.Infof("WOULD create a pull request for %s", currentBranch)
			return nil
		}

		// --head flag needs the branch to be pushed to the remote first.
		err := repo.Push(ctx, git.PushOptions{
			Remote:         "origin", // TODO: get remote from branch
			Refspec:        commitHash.String() + ":refs/heads/" + currentBranch,
			ForceWithLease: true,
		})
		if err != nil {
			return fmt.Errorf("push branch: %w", err)
		}

		// TODO: if current branch does not have an upstream set,
		// set the new remote branch as the upstream.

		// TODO: Use git push and GitHub API to create a pull request.
		// TODO: extract commit message for edit
		cmd := exec.Command("gh", "pr", "create", "--base", branch.Base, "--head", currentBranch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("create pull request: %w", err)
		}

	case 1:
		// Check base and HEAD are up-to-date.
		pull := pulls[0]
		var updates []string
		// TODO: update objects with String descriptions
		if pull.Head.GetSHA() != commitHash.String() {
			updates = append(updates, "push branch")
		}
		if pull.Base.GetRef() != branch.Base {
			updates = append(updates, "set base to "+branch.Base)
		}

		if len(updates) == 0 {
			log.Infof("Pull request #%d is up-to-date", pull.GetNumber())
			return nil
		}

		if cmd.DryRun {
			log.Infof("WOULD update PR #%d:", pull.GetNumber())
			for _, update := range updates {
				log.Infof("  - %s", update)
			}
			return nil
		}

		if pull.Head.GetSHA() != commitHash.String() {
			err := repo.Push(ctx, git.PushOptions{
				Remote:         "origin", // TODO: get remote from branch
				Refspec:        commitHash.String() + ":refs/heads/" + currentBranch,
				ForceWithLease: true,
			})
			if err != nil {
				return fmt.Errorf("push branch: %w", err)
			}
		}

		if pull.Base.GetRef() != branch.Base {
			_, _, err := gh.PullRequests.Edit(ctx, ghrepo.Owner, ghrepo.Name, pull.GetNumber(), &github.PullRequest{
				Base: &github.PullRequestBranch{
					Ref: &branch.Base,
				},
			})
			if err != nil {
				return fmt.Errorf("set base %v: %w", branch.Base, err)
			}
		}

		log.Infof("Updated pull request #%d: %s", pull.GetNumber(), pull.GetHTMLURL())

	default:
		// This is not allowed by GitHub.
		return fmt.Errorf("multiple open pull requests for %s", currentBranch)
	}

	return nil
}
