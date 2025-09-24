package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/list"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/secret"
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
		stash secret.Stash,
	) (ListHandler, error) {
		return &list.Handler{
			Log:        log,
			Repository: repo,
			Store:      store,
			Service:    svc,
			Forges:     forges,
			OpenRemoteRepository: func(ctx context.Context, f forge.Forge, repo forge.RepositoryID) (forge.Repository, error) {
				return openForgeRepository(ctx, stash, f, repo)
			},
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

	// Change state indicator styles: colored filled circle.
	_stateSymbol      = "◉"
	_stateOpenStyle   = ui.NewStyle().Foreground(ui.Green).SetString(_stateSymbol)
	_stateClosedStyle = ui.NewStyle().Foreground(ui.Gray).SetString(_stateSymbol)
	_stateMergedStyle = ui.NewStyle().Foreground(ui.Magenta).SetString(_stateSymbol)
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

	CRStatus bool `name:"cr-status" short:"S" config:"log.crStatus" help:"Request and include information about the Change Request" default:"false" negatable:""`
	// TODO: When needed, add a crStatusFormat config to control presentation.

	PushStatusFormat pushStatusFormat `config:"log.pushStatusFormat" help:"Show indicator for branches that are out of sync with their remotes." hidden:"" default:"true"`

	JSON bool `name:"json" released:"unreleased" help:"Write to stdout as a stream of JSON objects in an unspecified order"`
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

	var presenter logPresenter
	var wantChangeURL, wantPushStatus, wantChangeState bool
	if cmd.JSON {
		// JSON always wants URLs and push status, but respects --status for change state.
		wantChangeURL = true
		wantPushStatus = true
		wantChangeState = cmd.CRStatus

		presenter = &jsonLogPresenter{
			Stdout: kctx.Stdout,
		}
	} else {
		// Determine which ChangeFormat to use:
		// prefer long/short-specific, then fallback to general.
		changeFormat := cmd.ChangeFormat
		if opts.Commits && cmd.ChangeFormatLong != nil {
			changeFormat = *cmd.ChangeFormatLong
		} else if !opts.Commits && cmd.ChangeFormatShort != nil {
			changeFormat = *cmd.ChangeFormatShort
		}

		wantChangeURL = changeFormat == changeFormatURL
		wantPushStatus = cmd.PushStatusFormat.Enabled()
		wantChangeState = cmd.CRStatus
		presenter = &graphLogPresenter{
			Stderr:           kctx.Stderr,
			ChangeFormat:     changeFormat,
			ShowCRStatus:     wantChangeState,
			PushStatusFormat: cmd.PushStatusFormat,
		}
	}

	req := list.BranchesRequest{
		Branch:  currentBranch,
		Options: &cmd.Options,
	}
	if wantChangeURL {
		req.Include |= list.IncludeChangeURL
	}
	if wantChangeState {
		req.Include |= list.IncludeChangeState
	}
	if opts.Commits {
		req.Include |= list.IncludeCommits
	}
	if wantPushStatus {
		req.Include |= list.IncludePushStatus
	}

	res, err := listHandler.ListBranches(ctx, &req)
	if err != nil {
		return fmt.Errorf("log branches: %w", err)
	}

	return presenter.Present(res, currentBranch)
}

type logPresenter interface {
	Present(res *list.BranchesResponse, currentBranch string) error
}

type graphLogPresenter struct {
	Stderr           io.Writer        // required
	ChangeFormat     changeFormat     // required
	ShowCRStatus     bool             // required
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
				// Optional colored state indicator.
				var stateMark string
				if p.ShowCRStatus {
					switch b.ChangeState {
					case forge.ChangeOpen:
						stateMark = _stateOpenStyle.String()
					case forge.ChangeClosed:
						stateMark = _stateClosedStyle.String()
					case forge.ChangeMerged:
						stateMark = _stateMergedStyle.String()
					}
				}

				// Build CR text once, then append optional state symbol.
				var crText string
				switch p.ChangeFormat {
				case changeFormatID:
					crText = fmt.Sprintf("%v", cid)
				case changeFormatURL:
					crText = b.ChangeURL
				default:
					must.Failf("unknown change format: %v", p.ChangeFormat)
				}
				// Add state symbol if present.
				suffix := ""
				if stateMark != "" {
					suffix = " " + stateMark
				}
				_, _ = fmt.Fprintf(&o, " (%s%s)", crText, suffix)
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

type jsonLogPresenter struct {
	Stdout io.Writer // required
}

func (p *jsonLogPresenter) Present(res *list.BranchesResponse, currentBranch string) (retErr error) {
	bufw := bufio.NewWriter(p.Stdout)
	defer func() {
		retErr = errors.Join(retErr, bufw.Flush())
	}()

	enc := json.NewEncoder(bufw)
	for _, branch := range res.Branches {
		logBranch := jsonLogBranch{
			Name:    branch.Name,
			Current: branch.Name == currentBranch,
		}

		if branch.Base != "" {
			logBranch.Down = &jsonLogDown{
				Name:         branch.Base,
				NeedsRestack: branch.NeedsRestack,
			}
		}

		if len(branch.Aboves) > 0 {
			ups := make([]jsonLogUp, 0, len(branch.Aboves))
			for _, aboveIdx := range branch.Aboves {
				ups = append(ups, jsonLogUp{
					Name: res.Branches[aboveIdx].Name,
				})
			}
			logBranch.Ups = ups
		}

		if len(branch.Commits) > 0 {
			commits := make([]jsonLogCommit, 0, len(branch.Commits))
			for _, commit := range branch.Commits {
				commits = append(commits, jsonLogCommit{
					SHA:     commit.Hash.String(),
					Subject: commit.Subject,
				})
			}
			logBranch.Commits = commits
		}

		if branch.ChangeID != nil {
			jc := &jsonLogChange{
				ID:  branch.ChangeID.String(),
				URL: branch.ChangeURL,
			}
			if branch.ChangeState != 0 {
				switch branch.ChangeState {
				case forge.ChangeOpen:
					jc.Status = "open"
				case forge.ChangeClosed:
					jc.Status = "closed"
				case forge.ChangeMerged:
					jc.Status = "merged"
				}
			}
			logBranch.Change = jc
		}

		if status := branch.PushStatus; status != nil {
			logBranch.Push = &jsonLogPushStatus{
				Ahead:     status.Ahead,
				Behind:    status.Behind,
				NeedsPush: status.NeedsPush,
			}
		}

		if err := enc.Encode(logBranch); err != nil {
			return fmt.Errorf("encode branch %q: %w", branch.Name, err)
		}
	}

	return nil
}

type jsonLogBranch struct {
	// Name of the branch.
	Name string `json:"name"`

	// Current is true if this branch is the current branch.
	// This is false or omitted if this is not the current branch.
	Current bool `json:"current,omitempty"`

	// Down is the base branch onto which this branch is stacked.
	// This is unset if this branch is trunk.
	// 'gs down' from the current branch will check out this branch.
	Down *jsonLogDown `json:"down,omitempty"`

	// Ups is a list of branches that are stacked directly above this branch.
	// 'gs up' from this branch will check out one of these branches.
	Ups []jsonLogUp `json:"ups,omitempty"`

	// Commits is a list of commits on this branch.
	// These are not included unless invoked with 'gs log long'.
	Commits []jsonLogCommit `json:"commits,omitempty"`

	// Change is the associated change request, if any.
	// This is unset if this branch has not been published.
	Change *jsonLogChange `json:"change,omitempty"`

	// Push indicates the push status of this branch,
	// if the branch has been pushed to a remote.
	// This is unset if the branch has not been pushed
	// from git-spice's perspective.
	Push *jsonLogPushStatus `json:"push,omitempty"`
}

type jsonLogDown struct {
	// Name of the base branch.
	Name string `json:"name"`

	// NeedsRestack is true if the branch needs to be restacked
	// onto its base branch.
	NeedsRestack bool `json:"needsRestack,omitempty"`
}

type jsonLogUp struct {
	// Name of the branch stacked directly above this branch.
	Name string `json:"name"`
}

type jsonLogCommit struct {
	// SHA is the full commit hash.
	SHA string `json:"sha"`

	// Subject is the commit subject line.
	Subject string `json:"subject"`
}

type jsonLogChange struct {
	// ID is the change ID of the associated change.
	// For GitHub, this is the PR number.
	// For GitLab, this is the MR IID.
	ID string `json:"id"`

	// URL is the web URL of the associated change.
	URL string `json:"url"`

	// Status is the current state of the change (open|closed|merged).
	Status string `json:"status,omitempty"`
}

type jsonLogPushStatus struct {
	// Ahead is the number of commits that this branch is ahead
	// of its remote tracking branch.
	Ahead int `json:"ahead"`

	// Behind is the number of commits that this branch is behind
	// its remote tracking branch.
	Behind int `json:"behind"`

	// NeedsPush is true if this branch is out of sync with its remote,
	// and should be pushed.
	//
	// This will be false if Ahead and Behind are both zero.
	NeedsPush bool `json:"needsPush,omitempty"`
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
