package main

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
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

	_pushStatusStyle = ui.NewStyle().
				Foreground(ui.Yellow).
				Faint(true)

	_markerStyle = ui.NewStyle().
			Foreground(ui.Yellow).
			Bold(true).
			SetString("◀")
)

// branchLogCmd is the shared implementation of logShortCmd and logLongCmd.
type branchLogCmd struct {
	All bool `short:"a" long:"all" config:"log.all" help:"Show all tracked branches, not just the current stack."`

	ChangeFormat      string  `config:"log.crFormat" hidden:"" default:"id" enum:"id,url"`
	ChangeFormatShort *string `config:"logShort.crFormat" hidden:"" enum:"id,url"`
	ChangeFormatLong  *string `config:"logLong.crFormat" hidden:"" enum:"id,url"`

	PushStatusFormat pushStatusFormat `config:"log.pushStatusFormat" help:"Show indicator for branches that are out of sync with their remotes." hidden:"" default:"true"`
}

type branchLogOptions struct {
	Commits bool

	Log *silog.Logger
}

func (cmd *branchLogCmd) run(
	ctx context.Context,
	opts *branchLogOptions,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) (err error) {
	log := opts.Log
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // may be detached
	}

	allBranches, err := sliceutil.CollectErr(store.ListBranches(ctx))
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}
	sort.Strings(allBranches)

	getRemote := sync.OnceValue(func() string {
		remote, err := store.Remote()
		if err != nil {
			return ""
		}
		return remote
	})

	// Determine which ChangeFormat to use: prefer long/short-specific, then fallback to general.
	changeFormat := cmd.ChangeFormat
	if opts.Commits && cmd.ChangeFormatLong != nil {
		changeFormat = *cmd.ChangeFormatLong
	} else if !opts.Commits && cmd.ChangeFormatShort != nil {
		changeFormat = *cmd.ChangeFormatShort
	}

	var repoID forge.RepositoryID
	if changeFormat == "url" {
		err := func() error {
			remote := getRemote()

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

		// Number of commits ahead of the base and behind the head.
		Ahead, Behind int

		// Whether the branch needs to be pushed to its upstream.
		NeedsPush bool

		// Whether the branch needs to be restacked.
		NeedsRestack bool

		Commits []git.CommitDetail
		Aboves  []int
	}

	var infoMu sync.Mutex
	infos := make([]*branchInfo, len(allBranches)+1) // +1 for trunk
	infoIdxByName := make(map[string]int, len(allBranches))

	idxc := make(chan int) // index in allBranches
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for idx := range idxc {
				branchName := allBranches[idx]
				info := &branchInfo{
					Name: branchName,
				}

				// Check restack status /before/ looking up
				// the branch in git because VerifyRestacked
				// might update the branch's base hash
				// if the branch was manually restacked.
				//
				// TODO: This is a hack.
				// The isn't a good abstraction.
				var restackErr *spice.BranchNeedsRestackError
				if err := svc.VerifyRestacked(ctx, branchName); errors.As(err, &restackErr) {
					info.NeedsRestack = true
				}

				branch, err := svc.LookupBranch(ctx, branchName)
				if err != nil {
					log.Warn("Could not lookup branch", "branch", branchName, "error", err)
					continue
				}

				info.Base = branch.Base
				if branch.Change != nil {
					info.ChangeID = branch.Change.ChangeID()
				}

				if cmd.PushStatusFormat.Enabled() && branch.UpstreamBranch != "" {
					upstream := getRemote() + "/" + branch.UpstreamBranch
					ahead, behind, err := repo.CommitAheadBehind(ctx, upstream, string(branch.Head))
					if err == nil {
						info.Ahead = ahead
						info.Behind = behind
						info.NeedsPush = ahead > 0 || behind > 0
					}
				}

				if opts.Commits {
					commits, err := sliceutil.CollectErr(repo.ListCommitsDetails(ctx,
						git.CommitRangeFrom(branch.Head).
							ExcludeFrom(branch.BaseHash).
							FirstParent()))
					if err != nil {
						log.Warn("Could not list commits for branch. Skipping.", "branch", branchName, "error", err)
						continue
					}
					info.Commits = commits
				}

				infoMu.Lock()
				info.Index = idx
				infos[idx] = info
				infoIdxByName[branchName] = idx
				infoMu.Unlock()
			}
		}()
	}

	for idx := range allBranches {
		idxc <- idx
	}
	close(idxc)
	wg.Wait()

	trunkIdx := len(infos) - 1
	infos[trunkIdx] = &branchInfo{
		Index: trunkIdx,
		Name:  store.Trunk(),
	}
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
				switch changeFormat {
				case "id", "":
					_, _ = fmt.Fprintf(&o, " (%v)", cid)
				case "url":
					_, _ = fmt.Fprintf(&o, " (%s)", changeURL(cid))
				default:
					must.Failf("unknown change format: %v", cmd.ChangeFormat)
				}
			}

			if b.NeedsRestack {
				o.WriteString(_needsRestackStyle.String())
			}

			cmd.PushStatusFormat.FormatTo(&o, b.Ahead, b.Behind, b.NeedsPush)

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

// pushStatusFormat enumerates the possible values for the pushStatusFormat config.
type pushStatusFormat int

const (
	pushStatusEnabled     pushStatusFormat = iota // "(needs push)"
	pushStatusDisabled                            // show nothing
	pushStatusAheadBehind                         // show number of commits ahead/behind
)

var _ encoding.TextUnmarshaler = (*pushStatusFormat)(nil)

func (f *pushStatusFormat) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "true", "1", "yes":
		*f = pushStatusEnabled
	case "false", "0", "no":
		*f = pushStatusDisabled
	case "aheadbehind":
		*f = pushStatusAheadBehind
	default:
		return fmt.Errorf("invalid value %q: expected true, false, or aheadbehind", string(bs))
	}
	return nil
}

func (f pushStatusFormat) String() string {
	switch f {
	case pushStatusEnabled:
		return "true"
	case pushStatusDisabled:
		return "false"
	case pushStatusAheadBehind:
		return "aheadBehind"
	default:
		return "unknown"
	}
}

func (f pushStatusFormat) Enabled() bool {
	return f == pushStatusEnabled || f == pushStatusAheadBehind
}

func (f pushStatusFormat) FormatTo(sb *strings.Builder, ahead, behind int, needsPush bool) {
	switch f {
	case pushStatusEnabled:
		if needsPush {
			sb.WriteString(_pushStatusStyle.Render(" (needs push)"))
		}

	case pushStatusAheadBehind:
		if ahead == 0 && behind == 0 {
			break
		}

		// TODO: Should we support changing these symbols?
		var ab strings.Builder
		ab.WriteString(" (")
		if ahead > 0 {
			_, _ = fmt.Fprintf(&ab, "⇡%d", ahead)
		}
		if behind > 0 {
			_, _ = fmt.Fprintf(&ab, "⇣%d", behind)
		}
		ab.WriteString(")")

		sb.WriteString(_pushStatusStyle.Render(ab.String()))

	case pushStatusDisabled:
		// do nothing
	}
}
