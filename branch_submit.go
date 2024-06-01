package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
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

	Name string `arg:"" optional:"" placeholder:"BRANCH" help:"Branch to submit" predictor:"trackedBranches"`
}

func (*branchSubmitCmd) Help() string {
	return text.Dedent(`
		Creates or updates a Pull Request for the specified branch,
		or the current branch if none is specified.
		The Pull Request will use the tracked base branch
		as the merge base.

		For new Pull Requests, a prompt will allow filling metadata.
		Use the --title and --body flags to set the title and body,
		or the --fill flag to use the commit message to fill them in.
	`)
}

func (cmd *branchSubmitCmd) Run(
	ctx context.Context,
	log *log.Logger,
	opts *globalOptions,
	ghBuilder *github.Builder,
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

	svc := spice.NewService(repo, store, log)

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	branch, err := svc.LookupBranch(ctx, cmd.Name)
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

	// If the branch has already been pushed to upstream with a different name,
	// use that name instead.
	// This is useful for branches that were renamed locally.
	upstreamBranch := cmd.Name
	if branch.UpstreamBranch != "" {
		upstreamBranch = branch.UpstreamBranch
	}

	remote, err := ensureRemote(ctx, repo, store, log, opts)
	if err != nil {
		return err
	}

	forge, err := ensureGitHubForge(ctx, log, ghBuilder, repo, remote)
	if err != nil {
		return err
	}

	changes, err := forge.ListChanges(ctx, github.ListChangesOptions{
		State:  "open",
		Branch: upstreamBranch,
		// We don't filter by base here as that may be out of date.
	})
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}

	switch len(changes) {
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

		var defaultTitle, defaultBody string
		if len(msgs) == 1 {
			// If there's only one commit,
			// just the body will be the default body.
			defaultTitle = msgs[0].Subject
			defaultBody = msgs[0].Body
		} else {
			// Otherwise, we'll concatenate all the messages.
			// The revisions are in reverse order,
			// so we'll want to iterate in reverse.
			var body strings.Builder
			defaultTitle = msgs[len(msgs)-1].Subject
			for i := len(msgs) - 1; i >= 0; i-- {
				msg := msgs[i]
				if body.Len() > 0 {
					body.WriteString("\n\n")
				}
				body.WriteString(msg.Subject)
				if msg.Body != "" {
					body.WriteString("\n\n")
					body.WriteString(msg.Body)
				}
			}
			defaultBody = body.String()
		}

		var fields []ui.Field
		if cmd.Title == "" {
			cmd.Title = defaultTitle
			title := ui.NewInput().
				WithValue(&cmd.Title).
				WithTitle("Title").
				WithDescription("Short summary of the pull request").
				WithValidate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("title cannot be blank")
					}
					return nil
				})
			fields = append(fields, title)
		}

		if cmd.Body == "" {
			cmd.Body = defaultBody
			body := ui.NewOpenEditor().
				WithValue(&cmd.Body).
				WithTitle("Body").
				WithDescription("Open your editor to write " +
					"a detailed description of the pull request")
			// TODO: default body will also include the PR template
			// (if any) below the commit messages.
			// Querying for PR template requires GraphQL API.
			fields = append(fields, body)
		}

		if opts.Prompt {
			// TODO: default to true if subject is "WIP" or similar.
			draft := ui.NewConfirm().
				WithValue(&cmd.Draft).
				WithTitle("Draft").
				WithDescription("Mark the pull request as a draft?")
			fields = append(fields, draft)
		}

		// TODO: should we assume --fill if --no-prompt?
		if len(fields) > 0 && !cmd.Fill {
			if !opts.Prompt {
				return fmt.Errorf("prompt for commit information: %w", errNoPrompt)
			}

			form := ui.NewForm(fields...)
			if err := form.Run(); err != nil {
				return fmt.Errorf("prompt form: %w", err)
			}
		}
		must.NotBeBlankf(cmd.Title, "PR title must have been set")

		upsert := state.UpsertRequest{
			Name:           cmd.Name,
			UpstreamBranch: upstreamBranch,
		}

		err = repo.Push(ctx, git.PushOptions{
			Remote: remote,
			Refspec: git.Refspec(
				commitHash.String() + ":refs/heads/" + upstreamBranch,
			),
		})
		if err != nil {
			return fmt.Errorf("push branch: %w", err)
		}

		// At this point, even if any other operation fails,
		// we need to save to the state that we pushed the branch
		// with the recorded name.
		defer func() {
			err := store.Update(ctx, &state.UpdateRequest{
				Upserts: []state.UpsertRequest{upsert},
				Message: fmt.Sprintf("branch submit %s", cmd.Name),
			})
			if err != nil {
				log.Warn("Could not update state", "error", err)
			}
		}()

		upstream := remote + "/" + cmd.Name
		if err := repo.SetBranchUpstream(ctx, cmd.Name, upstream); err != nil {
			log.Warn("Could not set upstream", "branch", cmd.Name, "remote", remote, "error", err)
		}

		result, err := forge.SubmitChange(ctx, github.SubmitChangeRequest{
			Subject: cmd.Title,
			Body:    cmd.Body,
			Head:    cmd.Name,
			Base:    branch.Base,
			Draft:   cmd.Draft,
		})
		if err != nil {
			return fmt.Errorf("create change: %w", err)
		}
		upsert.PR = int(result.ID)

		log.Infof("Created %v: %s", result.ID, result.URL)

	case 1:
		// Check base and HEAD are up-to-date.
		pull := changes[0]
		var updates []string
		if pull.HeadHash != commitHash {
			updates = append(updates, "push branch")
		}
		if pull.BaseName != branch.Base {
			updates = append(updates, "set base to "+branch.Base)
		}
		if pull.Draft != cmd.Draft {
			updates = append(updates, "set draft to "+fmt.Sprint(cmd.Draft))
		}

		if len(updates) == 0 {
			log.Infof("Pull request %v is up-to-date", pull.ID)
			return nil
		}

		if cmd.DryRun {
			log.Infof("WOULD update PR %v:", pull.ID)
			for _, update := range updates {
				log.Infof("  - %s", update)
			}
			return nil
		}

		if pull.HeadHash != commitHash {
			err := repo.Push(ctx, git.PushOptions{
				Remote: remote,
				Refspec: git.Refspec(
					commitHash.String() + ":refs/heads/" + upstreamBranch,
				),
				// Force push, but only if the ref is exactly
				// where we think it is.
				ForceWithLease: cmd.Name + ":" + pull.HeadHash.String(),
			})
			if err != nil {
				log.Error("Branch may have been updated by someone else.")
				return fmt.Errorf("push branch: %w", err)
			}
		}

		if pull.BaseName != branch.Base || pull.Draft != cmd.Draft {
			opts := github.EditChangeOptions{
				Base: branch.Base,
			}
			if pull.Draft != cmd.Draft {
				opts.Draft = &cmd.Draft
			}

			if err := forge.EditChange(ctx, pull.ID, opts); err != nil {
				return fmt.Errorf("edit PR %v: %w", pull.ID, err)
			}
		}

		log.Infof("Updated %v: %s", pull.ID, pull.URL)

	default:
		// TODO: add a --pr flag to allow picking a PR?
		return fmt.Errorf("multiple open pull requests for %s", cmd.Name)
	}

	return nil
}
