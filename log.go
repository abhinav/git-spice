package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/fliptree"
	"go.abhg.dev/gs/internal/ui/widget"
)

type logCmd struct {
	Short logShortCmd `cmd:"" aliases:"s" help:"List branches"`
	Long  logLongCmd  `cmd:"" aliases:"l" help:"List branches and commits"`
}

var (
	_branchStyle        = ui.NewStyle().Bold(true)
	_currentBranchStyle = ui.NewStyle().
				Foreground(ui.Cyan).
				Bold(true)

	_logCommitStyle = widget.CommitSummaryStyle{
		Hash:    ui.NewStyle().Foreground(ui.Yellow),
		Subject: ui.NewStyle().Foreground(ui.Plain),
		Time:    ui.NewStyle().Foreground(ui.Gray),
	}
	_logCommitFaintStyle = _logCommitStyle.Faint(true)

	_needsRestackStyle = ui.NewStyle().
				Foreground(ui.Gray).
				SetString(" (needs restack)")

	_markerStyle = ui.NewStyle().
			Foreground(ui.Yellow).
			Bold(true).
			SetString("◀")
)

// branchLogCmd is the shared implementation of logShortCmd and logLongCmd.
type branchLogCmd struct {
	All          bool   `short:"a" long:"all" config:"log.all" help:"Show all tracked branches, not just the current stack."`
	ChangeFormat string `config:"log.crFormat" help:"Show URLs for branches with associated change requests." default:"id"`
}

type branchLogOptions struct {
	Commits bool

	Log *log.Logger
}

func (cmd *branchLogCmd) run(
	ctx context.Context,
	secretStash secret.Stash,
	opts *branchLogOptions,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) (err error) {
	log := opts.Log
	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // may be detached
	}

	allBranches, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}

	type branchInfo struct {
		Index    int
		Name     string
		Base     string
		ChangeID forge.ChangeID

		Commits []git.CommitDetail
		Aboves  []int
	}

	infos := make([]*branchInfo, 0, len(allBranches)+1) // +1 for trunk
	infoIdxByName := make(map[string]int, len(allBranches))
	for _, branch := range allBranches {
		info := &branchInfo{
			Name: branch.Name,
			Base: branch.Base,
		}
		if branch.Change != nil {
			info.ChangeID = branch.Change.ChangeID()
		}

		if opts.Commits {
			commits, err := repo.ListCommitsDetails(ctx,
				git.CommitRangeFrom(branch.Head).
					ExcludeFrom(branch.BaseHash).
					FirstParent())
			if err != nil {
				log.Warn("Could not list commits for branch. Skipping.", "branch", branch.Name, "err", err)
				continue
			}
			info.Commits = commits
		}

		idx := len(infos)
		info.Index = idx
		infos = append(infos, info)
		infoIdxByName[branch.Name] = idx
	}

	trunkIdx := len(infos)
	infos = append(infos, &branchInfo{
		Index: trunkIdx,
		Name:  store.Trunk(),
	})
	infoIdxByName[store.Trunk()] = trunkIdx

	// Second pass: Connect the "aboves".
	for idx, branch := range infos {
		if branch.Base == "" {
			continue
		}

		baseIdx, ok := infoIdxByName[branch.Base]
		if !ok {
			continue
		}

		infos[baseIdx].Aboves = append(infos[baseIdx].Aboves, idx)
	}

	isVisible := func(*branchInfo) bool { return true }
	if !cmd.All && currentBranch != "" {
		visible := make(map[int]struct{})
		currentBranchIdx := infoIdxByName[currentBranch]

		// Add the upstacks of the current branch to the visible set.
		for unseen := []int{currentBranchIdx}; len(unseen) > 0; {
			idx := unseen[len(unseen)-1]
			unseen = unseen[:len(unseen)-1]

			visible[idx] = struct{}{}
			unseen = append(unseen, infos[idx].Aboves...)
		}

		// Add the downstack of the current branch to the visible set.
		for idx, ok := currentBranchIdx, true; ok; idx, ok = infoIdxByName[infos[idx].Base] {
			visible[idx] = struct{}{}
		}

		isVisible = func(info *branchInfo) bool {
			_, ok := visible[info.Index]
			return ok
		}
	}

	treeStyle := fliptree.DefaultStyle[*branchInfo]()
	treeStyle.NodeMarker = func(b *branchInfo) lipgloss.Style {
		if b.Name == currentBranch {
			return fliptree.DefaultNodeMarker.SetString("■")
		}
		return fliptree.DefaultNodeMarker
	}

	// Get the remote repo if we need to print CR URLs
	var remoteRepo forge.Repository

	if cmd.ChangeFormat == "url" {
		remote, err := store.Remote()
		if err == nil {
			remoteRepo, err = openRemoteRepositorySilent(ctx, secretStash, forges, repo, remote)
			if err != nil {
				return fmt.Errorf("could not open remote repository; URLs will not be shown: %w", err)
			}
		}
	}

	var s strings.Builder
	err = fliptree.Write(&s, fliptree.Graph[*branchInfo]{
		Roots:  []int{trunkIdx},
		Values: infos,
		View: func(b *branchInfo) string {
			var o strings.Builder
			if b.Name == currentBranch {
				o.WriteString(_currentBranchStyle.Render(b.Name))
			} else {
				o.WriteString(_branchStyle.Render(b.Name))
			}

			if b.ChangeID != nil {
				if remoteRepo == nil {
					_, _ = fmt.Fprintf(&o, " (%v)", b.ChangeID)
				} else {
					if changeInfo, err := remoteRepo.FindChangeByID(ctx, b.ChangeID); err == nil && changeInfo != nil {
						_, _ = fmt.Fprintf(&o, " (%s)", changeInfo.URL)
					} else {
						_, _ = fmt.Fprintf(&o, " (%v)", b.ChangeID)
					}
				}
			}

			if restackErr := new(spice.BranchNeedsRestackError); errors.As(svc.VerifyRestacked(ctx, b.Name), &restackErr) {
				o.WriteString(_needsRestackStyle.String())
			}

			if b.Name == currentBranch {
				o.WriteString(" " + _markerStyle.String())
			}

			commitStyle := _logCommitStyle
			if b.Name != currentBranch {
				commitStyle = _logCommitFaintStyle
			}

			for _, commit := range b.Commits {
				o.WriteString("\n")
				(&widget.CommitSummary{
					ShortHash:  commit.ShortHash,
					Subject:    commit.Subject,
					AuthorDate: commit.AuthorDate,
				}).Render(&o, commitStyle)
			}

			return o.String()
		},
		Edges: func(bi *branchInfo) []int {
			aboves := make([]int, 0, len(bi.Aboves))
			for _, above := range bi.Aboves {
				if isVisible(infos[above]) {
					aboves = append(aboves, above)
				}
			}
			return aboves
		},
	}, fliptree.Options[*branchInfo]{Style: treeStyle})
	if err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	_, err = fmt.Fprint(os.Stderr, s.String())
	return err
}
