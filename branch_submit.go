package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"go.abhg.dev/gs/internal/gh"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
	"golang.org/x/oauth2"
)

type branchSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`

	Title string `help:"Title of the pull request (if creating one)"`
	Body  string `help:"Body of the pull request (if creating one)"`
	Draft bool   `help:"Mark the pull request as a draft"`
	Fill  bool   `help:"Fill in the pull request title and body from the commit messages"`
	// TODO: Default to Fill if --no-prompt

	// TODO: Other creation options e.g.:
	// - assignees
	// - labels
	// - milestone
	// - reviewers

	Name string `arg:"" optional:"" placeholder:"BRANCH" help:"Branch to submit"`
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

	svc := gs.NewService(repo, store, log)

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	branch, err := store.Lookup(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("lookup branch: %w", err)
	}

	// Refuse to submit if the branch is not restacked.
	if err := svc.VerifyRestacked(ctx, cmd.Name); err != nil {
		log.Errorf("Branch %s needs to be restacked.", cmd.Name)
		log.Errorf("Run the following command to fix this:")
		log.Errorf("  gs branch restack %s", cmd.Name)
		return errors.New("refusing to submit outdated branch")
		// TODO: this can be made optional with a --force or a prompt.
	}

	commitHash, err := repo.PeelToCommit(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	remote, err := store.Remote()
	if err != nil {
		if !errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("get remote: %w", err)
		}

		// No remote was specified at init time.
		// Guess or prompt for one and update the store.
		log.Warn("No remote was specified at init time")
		remote, err = (&gs.Guesser{
			Select: func(_ gs.GuessOp, opts []string, selected string) (string, error) {
				options := make([]huh.Option[string], len(opts))
				for i, opt := range opts {
					options[i] = huh.NewOption(opt, opt).
						Selected(opt == selected)
				}

				var result string
				prompt := huh.NewSelect[string]().
					Title("Please select the remote to which you'd like to push your changes").
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

	remoteURL, err := repo.RemoteURL(ctx, remote)
	if err != nil {
		return fmt.Errorf("get remote URL: %w", err)
	}

	// TODO: Take GITHUB_GIT_URL into account for ParseRepoInfo.
	ghrepo, err := gh.ParseRepoInfo(remoteURL)
	if err != nil {
		log.Error("Could not guess GitHub repository from remote URL", "url", remoteURL)
		log.Error("Are you sure the remote is a GitHub repository?")
		return err
	}

	gh := github.NewClient(oauth2.NewClient(ctx, tokenSource))
	if opts.GithubAPIURL != "" {
		gh, err = gh.WithEnterpriseURLs(opts.GithubAPIURL, gh.UploadURL.String())
		if err != nil {
			return fmt.Errorf("set GitHub API URL: %w", err)
		}
	}
	pulls, _, err := gh.PullRequests.List(ctx, ghrepo.Owner, ghrepo.Name, &github.PullRequestListOptions{
		State: "open",
		Head:  ghrepo.Owner + ":" + cmd.Name,
		// Don't filter by base -- we may need to update it.
	})
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}

	switch len(pulls) {
	case 0:
		if cmd.DryRun {
			log.Infof("WOULD create a pull request for %s", cmd.Name)
			return nil
		}

		msgs, err := repo.CommitMessageRange(ctx, cmd.Name, branch.Base)
		if err != nil {
			return fmt.Errorf("list commits: %w", err)
		}
		if len(msgs) == 0 {
			return errors.New("no commits to submit")
		}

		defaultTitle := msgs[0].Subject

		// If there's only one commit,
		// just the body will be the default body.
		// Otherwise, we'll concatenate all the messages.
		var defaultBody string
		if len(msgs) == 1 {
			defaultBody = msgs[0].Body
		} else {
			var body strings.Builder
			for i, msg := range msgs {
				if i > 0 {
					body.WriteString("\n\n")
				}
				body.WriteString(msg.Subject)
				body.WriteString("\n\n")
				body.WriteString(msg.Body)
			}
			defaultBody = body.String()
		}

		var fields []huh.Field
		if cmd.Title == "" {
			cmd.Title = defaultTitle
			title := huh.NewInput().
				Title("Title").
				Description("Short summary of the pull request").
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("title cannot be blank")
					}
					return nil
				}).
				Value(&cmd.Title)
			fields = append(fields, title.WithWidth(50))
		}
		if cmd.Body == "" {
			cmd.Body = defaultBody
			// TODO: replace with just a prompt to open the editor?
			body := huh.NewText().
				Title("Body").
				Description("Detailed description of the pull request").
				Value(&cmd.Body)
			fields = append(fields, body.WithWidth(72))
			// TODO: default body will also include the PR template
			// (if any) below the commit messages.
			// Querying for PR template requires GraphQL API.
		}
		if opts.Prompt {
			// TODO: default to true if subject is "WIP" or similar.
			body := huh.NewConfirm().
				Title("Draft").
				Description("Mark the pull request as a draft").
				Value(&cmd.Draft)
			fields = append(fields, body)
		}

		// TODO: should we assume --fill if --no-prompt?
		if len(fields) > 0 && !cmd.Fill {
			if !opts.Prompt {
				return fmt.Errorf("prompt for commit information: %w", errNoPrompt)
			}

			form := huh.NewForm(huh.NewGroup(fields...))
			if err := form.Run(); err != nil {
				return fmt.Errorf("prompt form: %w", err)
			}
		}
		must.NotBeBlankf(cmd.Title, "PR title must have been set")

		err = repo.Push(ctx, git.PushOptions{
			Remote: remote,
			Refspec: git.Refspec(
				commitHash.String() + ":refs/heads/" + cmd.Name,
			),
		})
		if err != nil {
			return fmt.Errorf("push branch: %w", err)
		}

		upstream := remote + "/" + cmd.Name
		if err := repo.SetBranchUpstream(ctx, cmd.Name, upstream); err != nil {
			log.Warn("Could not set upstream", "branch", cmd.Name, "remote", remote, "error", err)
		}

		pull, _, err := gh.PullRequests.Create(ctx, ghrepo.Owner, ghrepo.Name, &github.NewPullRequest{
			Title: &cmd.Title,
			Body:  &cmd.Body,
			Head:  &cmd.Name,
			Base:  &branch.Base,
			Draft: &cmd.Draft,
		})
		if err != nil {
			return fmt.Errorf("create pull request: %w", err)
		}

		log.Infof("Created #%d: %s", pull.GetNumber(), pull.GetHTMLURL())

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
		if pull.GetDraft() != cmd.Draft {
			updates = append(updates, "set draft to "+fmt.Sprint(cmd.Draft))
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
				Remote: remote,
				Refspec: git.Refspec(
					commitHash.String() + ":refs/heads/" + cmd.Name,
				),
				// Force push, but only if the ref is exactly
				// where we think it is.
				ForceWithLease: cmd.Name + ":" + pull.Head.GetSHA(),
			})
			if err != nil {
				log.Error("Branch may have been updated by someone else.")
				return fmt.Errorf("push branch: %w", err)
			}
		}

		if pull.Base.GetRef() != branch.Base || pull.GetDraft() != cmd.Draft {
			_, _, err := gh.PullRequests.Edit(ctx, ghrepo.Owner, ghrepo.Name, pull.GetNumber(), &github.PullRequest{
				Base: &github.PullRequestBranch{
					Ref: &branch.Base,
				},
				Draft: &cmd.Draft,
			})
			if err != nil {
				return fmt.Errorf("update PR #%d: %w", pull.GetNumber(), err)
			}
		}

		log.Infof("Updated #%d: %s", pull.GetNumber(), pull.GetHTMLURL())

	default:
		// TODO: add a --pr flag to allow picking a PR?
		return fmt.Errorf("multiple open pull requests for %s", cmd.Name)
	}

	return nil
}
