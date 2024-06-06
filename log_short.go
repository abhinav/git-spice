package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/fliptree"
)

var (
	_currentBranchStyle = lipgloss.NewStyle().
				Foreground(ui.Cyan).
				Bold(true)

	_needsRestackStyle = lipgloss.NewStyle().
				Foreground(ui.Gray).
				SetString(" (needs restack)")

	_markerStyle = lipgloss.NewStyle().
			Foreground(ui.Yellow).
			Bold(true).
			SetString("◀")
)

type logShortCmd struct {
	All bool `short:"a" long:"all" help:"Show all tracked branches, not just the current stack."`
}

func (*logShortCmd) Help() string {
	return text.Dedent(`
		Provides a tree view of the branches in the current stack,
		both upstack and downstack from it.
		Use with the -a flag to show all tracked branches.
	`)
}

func (cmd *logShortCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // may be detached
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := spice.NewService(repo, store, log)
	allBranches, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}

	type trackedBranch struct {
		Aboves []string
		Base   string
		PR     int
	}

	infos := make(map[string]*trackedBranch, len(allBranches))
	branchInfo := func(branch string) *trackedBranch {
		if info, ok := infos[branch]; ok {
			return info
		}
		info := &trackedBranch{}
		infos[branch] = info
		return info
	}

	for _, branch := range allBranches {
		b := branchInfo(branch.Name)
		b.Base = branch.Base
		b.PR = branch.PR

		base := branchInfo(branch.Base)
		base.Aboves = append(base.Aboves, branch.Name)
	}

	edgesFn := func(branch string) []string {
		return branchInfo(branch).Aboves
	}

	if !cmd.All {
		reachable := make(map[string]struct{})
		for unseen := []string{currentBranch}; len(unseen) > 0; {
			branch := unseen[len(unseen)-1]
			unseen = unseen[:len(unseen)-1]
			reachable[branch] = struct{}{}
			unseen = append(unseen, branchInfo(branch).Aboves...)
		}
		for b := currentBranch; b != ""; b = branchInfo(b).Base {
			reachable[b] = struct{}{}
		}

		oldBranchesAbove := edgesFn
		edgesFn = func(branch string) []string {
			var bs []string
			for _, b := range oldBranchesAbove(branch) {
				if _, ok := reachable[b]; ok {
					bs = append(bs, b)
				}
			}
			return bs
		}
	}

	var s strings.Builder
	err = fliptree.Write(&s, fliptree.Graph{
		Roots: []string{store.Trunk()},
		View: func(branch string) string {
			var o strings.Builder
			if branch == currentBranch {
				o.WriteString(_currentBranchStyle.Render(branch))
			} else {
				o.WriteString(branch)
			}

			info := branchInfo(branch)
			if info.PR != 0 {
				_, _ = fmt.Fprintf(&o, " (#%d)", info.PR)
			}

			if restackErr := new(spice.BranchNeedsRestackError); errors.As(svc.VerifyRestacked(ctx, branch), &restackErr) {
				o.WriteString(_needsRestackStyle.String())
			}

			if branch == currentBranch {
				o.WriteString(" " + _markerStyle.String())
			}

			return o.String()
		},
		Edges: edgesFn,
	}, fliptree.Options{})
	if err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	_, err = fmt.Fprint(os.Stderr, s.String())
	return err
}
