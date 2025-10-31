package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/text"
)

// submitOptions defines options that are common to all submit commands.
type submitOptions struct {
	submit.Options

	NoWeb bool `help:"Alias for --web=false."`

	// TODO: Other creation options e.g.:
	// - assignees
	// - labels
	// - milestone
	// - reviewers
}

const _submitHelp = `
Use --dry-run to print what would be submitted without submitting it.

For new Change Requests, a prompt will allow filling metadata.
Use --fill to populate title and body from the commit messages.
The --[no-]draft flag marks the CR as draft or not.
Use the 'spice.submit.draft' configuration option
to mark new CRs as drafts (or not) by default,
skipping the prompt.

For updating Change Requests,
use --[no-]draft to change its draft status.
Without the flag, the draft status is not changed.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.

Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments
only if there are multiple CRs in the stack.
`

type branchSubmitCmd struct {
	submitOptions

	Title  string `help:"Title of the change request" placeholder:"TITLE"`
	Body   string `help:"Body of the change request" placeholder:"BODY"`
	Branch string `placeholder:"NAME" help:"Branch to submit" predictor:"trackedBranches"`
}

func (*branchSubmitCmd) Help() string {
	return text.Dedent(`
		A Change Request is created for the current branch,
		or updated if it already exists.
		Use the --branch flag to target a different branch.

		For new Change Requests, a prompt will allow filling metadata.
		Use the --title and --body flags to skip the prompt,
		or the --fill flag to use the commit message to fill them in.
		The --[no-]draft flag marks the CR as draft or not.
		Use the 'spice.submit.draft' configuration option
		to mark new CRs as drafts (or not) by default,
		skipping the prompt.

		For updating Change Requests,
		use --[no-]draft to change its draft status.
		Without the flag, the draft status is not changed.

		Use --no-publish to push branches without creating CRs.
		This has no effect if a branch already has an open CR.

		Use --update-only to only update branches with existing CRs,
		and skip those that would create new CRs.

		Use --nav-comment=false to disable navigation comments in CRs,
		or --nav-comment=multiple to post those comments
		only if there are multiple CRs in the stack.
	`)
}

// SubmitHandler submits change requests to a forge.
type SubmitHandler interface {
	Submit(ctx context.Context, req *submit.Request) error
	SubmitBatch(ctx context.Context, req *submit.BatchRequest) error
}

func (cmd *branchSubmitCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	submitHandler SubmitHandler,
) error {
	if cmd.NoWeb {
		cmd.Web = submit.OpenWebNever
	}

	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	return submitHandler.Submit(ctx, &submit.Request{
		Branch:  cmd.Branch,
		Title:   cmd.Title,
		Body:    cmd.Body,
		Options: &cmd.Options,
	})
}
