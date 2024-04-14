package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/gh"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/state"
	"golang.org/x/oauth2"
)

type branchSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
}

func (cmd *branchSubmitCmd) Run(
	ctx context.Context,
	log *log.Logger,
	opts *globalOptions,
	tokenSource oauth2.TokenSource,
) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
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

	remote, err := store.Remote()
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			// No remote was specified at init time.
			// Prompt for one and update internal state.
			remote, err := (&gs.Guesser{
				Select: func(_ gs.GuessOp, opts []string, selected string) (string, error) {
					options := make([]huh.Option[string], len(opts))
					for i, opt := range opts {
						options[i] = huh.NewOption(opt, opt).
							Selected(opt == selected)
					}

					var result string
					prompt := huh.NewSelect[string]().
						Title("Please select the remote to which you'd like to push your changes").
						Description("No remote was specified at init time").
						Options(options...).
						Value(&result)
					err := prompt.Run()
					return result, err
				},
			}).GuessRemote(ctx, repo)
			if err != nil {
				return fmt.Errorf("guess remote: %w", err)
			}

			if err := store.SetRemote(ctx, remote); err != nil {
				return fmt.Errorf("set remote: %w", err)
			}

			log.Infof("Changed repository remote to %s", remote)
		}
		return fmt.Errorf("get remote: %w", err)
	}

	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return fmt.Errorf("get remote URL: %w", err)
	}

	ghrepo, err := gh.ParseRepoInfo(remoteURL)
	if err != nil {
		log.Error("Could not guess GitHub repository from remote URL", "url", remoteURL)
		log.Error("Are you sure the remote is a GitHub repository?")
		return fmt.Errorf("parse GitHub repository: %w", err)
	}

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
			Remote:         remote,
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
				Remote:         remote,
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
