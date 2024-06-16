package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/fliptree"
)

type logCmd struct {
	Short logShortCmd `cmd:"" aliases:"s" help:"Short view of stack"`
}

var (
	_branchStyle = ui.NewStyle().Bold(true)

	_currentBranchStyle = ui.NewStyle().
				Foreground(ui.Cyan).
				Bold(true)

	_needsRestackStyle = ui.NewStyle().
				Foreground(ui.Gray).
				SetString(" (needs restack)")

	_markerStyle = ui.NewStyle().
			Foreground(ui.Yellow).
			Bold(true).
			SetString("◀")
)

type branchLogOptions struct {
	All     bool
	Commits bool

	Log     *log.Logger
	Globals *globalOptions
}

func branchLog(ctx context.Context, opts *branchLogOptions) (err error) {
	repo, store, svc, err := openRepo(ctx, opts.Log, opts.Globals)
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // may be detached
	}

	allBranches, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}

	type trackedBranch struct {
		Aboves   []string
		Base     string
		ChangeID forge.ChangeID

		HeadHash git.Hash
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
		if md := branch.Change; md != nil {
			b.ChangeID = md.ChangeID()
		}

		base := branchInfo(branch.Base)
		base.Aboves = append(base.Aboves, branch.Name)
	}

	edgesFn := func(branch string) []string {
		return branchInfo(branch).Aboves
	}

	var listBranches []string
	if opts.All {
		for branch := range infos {
			listBranches = append(listBranches, branch)
		}
		sort.Strings(listBranches)
	} else {
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

		for b := range reachable {
			listBranches = append(listBranches, b)
		}
		sort.Strings(listBranches)

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

	treeStyle := fliptree.DefaultStyle()
	treeStyle.NodeMarker = func(branch string) lipgloss.Style {
		if branch == currentBranch {
			return fliptree.DefaultNodeMarker.SetString("■")
		}
		return fliptree.DefaultNodeMarker
	}

	var s strings.Builder
	// TODO: Maybe Graph is parameterized over the node type.
	err = fliptree.Write(&s, fliptree.Graph{
		Roots: []string{store.Trunk()},
		View: func(branch string) string {
			var o strings.Builder
			if branch == currentBranch {
				o.WriteString(_currentBranchStyle.Render(branch))
			} else {
				o.WriteString(_branchStyle.Render(branch))
			}

			info := branchInfo(branch)
			if info.ChangeID != nil {
				_, _ = fmt.Fprintf(&o, " (%v)", info.ChangeID)
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
	}, fliptree.Options{Style: treeStyle})
	if err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	_, err = fmt.Fprint(os.Stderr, s.String())
	return err
}
