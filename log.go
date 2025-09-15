package main

import (
	"cmp"
	"context"
	"encoding"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/list"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
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

func (*logCmd) AfterApply(kctx *kong.Context) error {
	return kctx.BindToProvider(func(
		log *silog.Logger,
		repo *git.Repository,
		store *state.Store,
		svc *spice.Service,
		forges *forge.Registry,
	) (ListHandler, error) {
		return &list.Handler{
			Log:        log,
			Repository: repo,
			Store:      store,
			Service:    svc,
			Forges:     forges,
		}, nil
	})
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

type ListHandler interface {
	ListBranches(context.Context, *list.BranchesRequest) (*list.BranchesResponse, error)
}

// branchLogCmd is the shared implementation of logShortCmd and logLongCmd.
type branchLogCmd struct {
	list.Options

	ChangeFormat      changeFormat  `config:"log.crFormat" hidden:"" default:"id"`
	ChangeFormatShort *changeFormat `config:"logShort.crFormat" hidden:""`
	ChangeFormatLong  *changeFormat `config:"logLong.crFormat" hidden:""`

	PushStatusFormat pushStatusFormat `config:"log.pushStatusFormat" help:"Show indicator for branches that are out of sync with their remotes." hidden:"" default:"true"`
}

type branchLogOptions struct {
	Commits bool
}

func (cmd *branchLogCmd) run(
	ctx context.Context,
	kctx *kong.Context,
	opts *branchLogOptions,
	wt *git.Worktree,
	listHandler ListHandler,
) (err error) {
	opts = cmp.Or(opts, &branchLogOptions{})

	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // may be detached
	}

	req := list.BranchesRequest{
		Branch:  currentBranch,
		Options: &cmd.Options,
	}
	// Determine which ChangeFormat to use:
	// prefer long/short-specific, then fallback to general.
	changeFormat := cmd.ChangeFormat
	if opts.Commits && cmd.ChangeFormatLong != nil {
		changeFormat = *cmd.ChangeFormatLong
	} else if !opts.Commits && cmd.ChangeFormatShort != nil {
		changeFormat = *cmd.ChangeFormatShort
	}
	if changeFormat == changeFormatURL {
		req.Include |= list.IncludeChangeURL
	}
	if opts.Commits {
		req.Include |= list.IncludeCommits
	}
	if cmd.PushStatusFormat.Enabled() {
		req.Include |= list.IncludePushStatus
	}

	res, err := listHandler.ListBranches(ctx, &req)
	if err != nil {
		return fmt.Errorf("log branches: %w", err)
	}

	// TODO: JSON presenter
	var presenter logPresenter = &graphLogPresenter{
		Stderr:           kctx.Stderr,
		ChangeFormat:     changeFormat,
		PushStatusFormat: cmd.PushStatusFormat,
	}

	return presenter.Present(res, currentBranch)
}

type logPresenter interface {
	Present(res *list.BranchesResponse, currentBranch string) error
}

type graphLogPresenter struct {
	Stderr           io.Writer        // required
	ChangeFormat     changeFormat     // required
	PushStatusFormat pushStatusFormat // required
}

func (p *graphLogPresenter) Present(res *list.BranchesResponse, currentBranch string) error {
	treeStyle := fliptree.DefaultStyle[*list.BranchItem]()
	treeStyle.NodeMarker = func(b *list.BranchItem) lipgloss.Style {
		if b.Name == currentBranch {
			return fliptree.DefaultNodeMarker.SetString("■")
		}
		return fliptree.DefaultNodeMarker
	}

	var s strings.Builder
	err := fliptree.Write(&s, fliptree.Graph[*list.BranchItem]{
		Roots:  []int{res.TrunkIdx},
		Values: res.Branches,
		View: func(b *list.BranchItem) string {
			var o strings.Builder
			if b.Name == currentBranch {
				o.WriteString(_currentBranchStyle.Render(b.Name))
			} else {
				o.WriteString(_branchStyle.Render(b.Name))
			}

			if cid := b.ChangeID; cid != nil {
				switch p.ChangeFormat {
				case changeFormatID:
					_, _ = fmt.Fprintf(&o, " (%v)", cid)
				case changeFormatURL:
					_, _ = fmt.Fprintf(&o, " (%s)", b.ChangeURL)
				default:
					must.Failf("unknown change format: %v", p.ChangeFormat)
				}
			}

			if b.NeedsRestack {
				o.WriteString(_needsRestackStyle.String())
			}

			if s := b.PushStatus; s != nil {
				p.PushStatusFormat.FormatTo(&o, s.Ahead, s.Behind, s.NeedsPush)
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
		Edges: func(bi *list.BranchItem) []int {
			return bi.Aboves
		},
	}, fliptree.Options[*list.BranchItem]{Style: treeStyle})
	if err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	_, err = fmt.Fprint(p.Stderr, s.String())
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

// changeFormat enumerates the possible values for the changeFormat config.
type changeFormat int

const (
	changeFormatID  changeFormat = iota // "id"
	changeFormatURL                     // "url"
)

var _ encoding.TextUnmarshaler = (*changeFormat)(nil)

func (f *changeFormat) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "id":
		*f = changeFormatID
	case "url":
		*f = changeFormatURL
	default:
		return fmt.Errorf("invalid value %q: expected id or url", string(bs))
	}
	return nil
}

func (f changeFormat) String() string {
	switch f {
	case changeFormatID:
		return "id"
	case changeFormatURL:
		return "url"
	default:
		return fmt.Sprintf("changeFormat(%d)", int(f))
	}
}
