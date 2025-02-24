package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type branchCmd struct {
	Track    branchTrackCmd    `cmd:"" aliases:"tr" help:"Track a branch"`
	Untrack  branchUntrackCmd  `cmd:"" aliases:"untr" help:"Forget a tracked branch"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Switch to a branch"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"d,rm" help:"Delete branches"`
	Fold   branchFoldCmd   `cmd:"" aliases:"fo" help:"Merge a branch into its base"`
	Split  branchSplitCmd  `cmd:"" aliases:"sp" help:"Split a branch on commits"`
	Squash branchSquashCmd `cmd:"" aliases:"sq" help:"Squash a branch into one commit" released:"v0.11.0"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the commits in a branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"rn,mv" help:"Rename a branch"`
	Restack branchRestackCmd `cmd:"" aliases:"r" help:"Restack a branch"`
	Onto    branchOntoCmd    `cmd:"" aliases:"on" help:"Move a branch onto another branch"`

	// Pull request management
	Submit branchSubmitCmd `cmd:"" aliases:"s" help:"Submit a branch"`
}

// BranchPromptConfig defines configuration for the branch tree prompt
// presented from commands that need the user to select a branch interactively.
//
// Embed this in commands that need to use the prompt
// and a *branchPrompter will be injected into the Kong context.
type BranchPromptConfig struct {
	// Verbose names for flags to avoid conflicting with command flags.
	//
	// hidden:"" means that the CLI flag isn't intended to be used.
	// Only the configuration.

	BranchPromptSort string `hidden:"" config:"branchPrompt.sort" help:"Sort branches by the given field"`
}

// BeforeApply is called by Kong as part of parsing.
// This is the earliest hook we can introduce the binding in.
func (cfg *BranchPromptConfig) BeforeApply(kctx *kong.Context) error {
	return kctx.BindToProvider(onceFunc(func(view ui.View, repo *git.Repository, store *state.Store) (*branchPrompter, error) {
		return &branchPrompter{
			sort:  cfg.BranchPromptSort,
			view:  view,
			repo:  repo,
			store: store,
		}, nil
	}))
}

// branchPrompter presents the user with an interactive prompt
// to select a branch from a list of local branches.
//
// Tracked branches are presented in a tree view.
type branchPrompter struct {
	// sort order for branches globally.
	// Defaults to branch name if unset.
	sort string

	view  ui.View
	repo  *git.Repository
	store *state.Store
}

// branchPromptRequest defines parameters for the branch prompt
// presented to the user to select a local branch
// that may or may not be tracked by the store.
type branchPromptRequest struct {
	// Disabled specifies whether the given branch is selectable.
	Disabled func(git.LocalBranch) bool

	// TrackedOnly indicates that only tracked branches and Trunk
	// should be included in the list.
	TrackedOnly bool

	// Default specifies the branch to select by default.
	Default string

	// Title specifies the prompt to display to the user.
	Title string

	// Description specifies the description to display to the user.
	Description string
}

func (p *branchPrompter) Prompt(ctx context.Context, req *branchPromptRequest) (string, error) {
	disabled := func(git.LocalBranch) bool { return false }
	if req.Disabled != nil {
		disabled = req.Disabled
		// TODO: allow disabled branches to specify a reason.
		// Can be used to say "(checked out elsewhere)" or similar.
	}

	trunk := p.store.Trunk()
	filter := func(git.LocalBranch) bool { return true }
	if req.TrackedOnly {
		tracked, err := p.store.ListBranches(ctx)
		if err != nil {
			return "", fmt.Errorf("list tracked branches: %w", err)
		}
		slices.Sort(tracked)

		oldFilter := filter
		filter = func(b git.LocalBranch) bool {
			if b.Name == trunk {
				// Always consider Trunk tracked.
				return oldFilter(b)
			}

			_, ok := slices.BinarySearch(tracked, b.Name)
			return ok && oldFilter(b)
		}
	}

	localBranches, err := p.repo.LocalBranches(ctx, &git.LocalBranchesOptions{
		Sort: p.sort,
	})
	if err != nil {
		return "", fmt.Errorf("list branches: %w", err)
	}

	bases := make(map[string]string) // branch -> base
	for _, branch := range localBranches {
		res, err := p.store.LookupBranch(ctx, branch.Name)
		if err == nil {
			bases[branch.Name] = res.Base
		}
	}

	items := make([]widget.BranchTreeItem, 0, len(localBranches))
	for _, branch := range localBranches {
		if !filter(branch) {
			continue
		}
		items = append(items, widget.BranchTreeItem{
			Base:     bases[branch.Name],
			Branch:   branch.Name,
			Disabled: disabled(branch),
		})
	}

	value := req.Default
	prompt := widget.NewBranchTreeSelect().
		WithTitle(req.Title).
		WithValue(&value).
		WithItems(items...).
		WithDescription(req.Description)
	if err := ui.Run(p.view, prompt); err != nil {
		return "", fmt.Errorf("select branch: %w", err)
	}

	return value, nil
}
