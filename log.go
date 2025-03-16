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
	"go.abhg.dev/gs/internal/must"
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

	_pushedStyle = ui.NewStyle().
			Foreground(ui.Green)

	_markerStyle = ui.NewStyle().
			Foreground(ui.Yellow).
			Bold(true).
			SetString("◀")
)

// branchLogCmd is the shared implementation of logShortCmd and logLongCmd.
type branchLogCmd struct {
	All          bool   `short:"a" long:"all" config:"log.all" help:"Show all tracked branches, not just the current stack."`
	ChangeFormat string `config:"log.crFormat" help:"Show URLs for branches with associated change requests." hidden:"" default:"id" enum:"id,url"`
	PushedFormat bool   `config:"log.pushedFormat" help:"Show indicator for branches that are synced with their remote." hidden:"" default:"true"`
}

type branchLogOptions struct {
	Commits bool

	Log *log.Logger
}

func (cmd *branchLogCmd) run(
	ctx context.Context,
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

	var repoID forge.RepositoryID
	if cmd.ChangeFormat == "url" {
		err := func() error {
			remote, err := store.Remote()
			if err != nil {
				// No remote to match against. Not an error.
				return nil
			}

			remoteURL, err := repo.RemoteURL(ctx, remote)
			if err != nil {
				return fmt.Errorf("get remote URL: %w", err)
			}

			var ok bool
			_, repoID, ok = forge.MatchRemoteURL(forges, remoteURL)
			if !ok {
				return fmt.Errorf("no forge matches remote URL %q", remoteURL)
			}

			return nil
		}()
		if err != nil {
			log.Warn("Could not find information about the remote", "error", err)
		}
	}

	// changeURL queries the forge for the URL of a change request.
	changeURL := func(changeID forge.ChangeID) string {
		if repoID == nil {
			// No forge to query against. Just return the change ID.
			return changeID.String()
		}

		return repoID.ChangeURL(changeID)
	}

	type branchInfo struct {
		Index    int
		Name     string
		Base     string
		ChangeID forge.ChangeID
		IsPushed bool

		Commits []git.CommitDetail
		Aboves  []int
	}

	// Get the remote name for checking branch sync status
	var remote string

	if cmd.PushedFormat {
		if remote, err = store.Remote(); err != nil {
			remote = ""
		}
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

		if cmd.PushedFormat && remote != "" && branch.UpstreamBranch != "" {
			upstream := remote + "/" + branch.UpstreamBranch
			if hash, err := repo.PeelToCommit(ctx, upstream); err == nil {
				info.IsPushed = branch.Head == hash
			}
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

			if cid := b.ChangeID; cid != nil {
				switch cmd.ChangeFormat {
				case "id", "":
					_, _ = fmt.Fprintf(&o, " (%v)", cid)
				case "url":
					_, _ = fmt.Fprintf(&o, " (%s)", changeURL(cid))
				default:
					must.Failf("unknown change format: %v", cmd.ChangeFormat)
				}
			}

			if restackErr := new(spice.BranchNeedsRestackError); errors.As(svc.VerifyRestacked(ctx, b.Name), &restackErr) {
				o.WriteString(_needsRestackStyle.String())
			}

			if cmd.PushedFormat && b.IsPushed {
				o.WriteString(_pushedStyle.SetString(" (pushed)").String())
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
