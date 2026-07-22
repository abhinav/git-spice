package submit

import (
	"encoding"
	"fmt"
	"slices"
	"time"

	"go.abhg.dev/gs/internal/iterutil"
)

// Options defines options for the submit operations.
//
// Options translate into user-facing command line flags or configuration options,
// so care must be taken when adding things here.
type Options struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `short:"c" help:"Fill in the change title and body from the commit messages"`
	// TODO: Default to Fill if --no-prompt?
	Draft   *bool   `negatable:"" help:"Whether to mark change requests as drafts"`
	Publish bool    `name:"publish" negatable:"" default:"true" config:"submit.publish" help:"Whether to create CRs for pushed branches. Defaults to true."`
	Web     OpenWeb `short:"w" config:"submit.web" help:"Open submitted changes in a web browser. Accepts an optional argument: 'true', 'false', 'created'."`

	NavComment          NavCommentWhen      `name:"nav-comment" config:"submit.navigationComment" enum:"true,false,multiple" default:"true" help:"Whether to add a navigation comment to the change request. Must be one of: true, false, multiple."`
	NavCommentSync      NavCommentSync      `name:"nav-comment-sync" config:"submit.navigationCommentSync" enum:"branch,downstack" default:"branch" hidden:"" help:"Which navigation comment to sync. Must be one of: branch, downstack."`
	NavCommentDownstack NavCommentDownstack `name:"nav-comment-downstack" config:"submit.navigationComment.downstack" enum:"all,open" default:"all" hidden:"" help:"Which downstack CRs to include in navigation comments. Must be one of: all, open."`
	NavCommentMarker    string              `name:"nav-comment-marker" config:"submit.navigationCommentStyle.marker" hidden:"" help:"Marker to use for the current change in navigation comments. Defaults to '◀'."`

	NavCommentTrunkLink     NavCommentTrunkLink `name:"nav-comment-trunk-link" config:"submit.navigationComment.trunkComparison" enum:"false,top,all" default:"false" hidden:"" help:"Whether to include a link comparing the branch against trunk in navigation comments, and on which CRs. Must be one of: false, top, all."`
	NavCommentTrunkLinkText string              `name:"nav-comment-trunk-link-text" config:"submit.navigationCommentStyle.trunkComparisonText" hidden:"" help:"Text for the trunk comparison link in navigation comments. Defaults to 'Compare against trunk'."`

	SkipRestackCheck SkipRestackCheck `config:"submit.skipRestackCheck" hidden:"" help:"When to skip the restack check. Must be one of: never, trunk, always." default:"never"`

	Force      bool  `help:"Force push, bypassing safety checks"`
	NoVerify   bool  `help:"Bypass pre-push hooks when pushing to the remote." released:"v0.15.0"`
	UpdateOnly *bool `short:"u" negatable:"" help:"Only update existing change requests, do not create new ones"`

	// DraftDefault is used to set the default draft value
	// when creating new Change Requests.
	//
	// --draft/--no-draft will override this value.
	DraftDefault bool `config:"submit.draft" help:"Default value for --draft when creating change requests." hidden:"" default:"false"`

	// TODO: Other creation options e.g.:
	// - milestone
	// - reviewers

	Labels           []string      `name:"label" short:"l" help:"Add labels to the change request. Pass multiple times or separate with commas."`
	ConfiguredLabels []string      `name:"configured-labels" help:"Default labels to add to change requests." hidden:"" config:"submit.labels" configDeprecated:"submit.label"`
	LabelsAddWhen    LabelsAddWhen `name:"labels-add-when" help:"When to add configured labels." hidden:"" config:"submit.labels.addWhen" configDeprecated:"submit.label.addWhen" default:"always"`

	Reviewers           []string         `short:"r" name:"reviewer" help:"Add reviewers to the change request. Pass multiple times or separate with commas." released:"v0.21.0"`
	ConfiguredReviewers []string         `name:"configured-reviewers" help:"Default reviewers to add to change requests." hidden:"" config:"submit.reviewers" released:"v0.21.0"`
	ReviewersAddWhen    ReviewersAddWhen `name:"reviewers-add-when" help:"When to add configured reviewers." hidden:"" config:"submit.reviewers.addWhen" default:"always" released:"v0.23.0"`

	Assignees           []string `short:"a" name:"assign" placeholder:"ASSIGNEE" help:"Assign the change request to these users. Pass multiple times or separate with commas." released:"v0.21.0"`
	ConfiguredAssignees []string `name:"configured-assignees" help:"Default assignees to add to change requests." hidden:"" config:"submit.assignees" released:"v0.21.0"` // merged with Assignees

	// ListTemplatesTimeout controls the timeout for listing CR templates.
	ListTemplatesTimeout time.Duration `hidden:"" config:"submit.listTemplatesTimeout" help:"Timeout for listing CR templates" default:"1s"`

	// Template specifies the template to use when multiple templates are available.
	// If set, this template will be automatically selected instead of prompting the user.
	// The value should match the filename of one of the available templates.
	Template string `hidden:"" config:"submit.template" help:"Default template to use when multiple templates are available"`
}

func mergeConfiguredValues(values []string, configured []string) []string {
	return slices.Collect(iterutil.Uniq(values, configured))
}

func mergeConfiguredOptions(opts *Options) {
	// Note: Labels are merged conditionally by effectiveLabels
	// based on LabelsAddWhen setting.
	// Note: Reviewers are merged conditionally by effectiveReviewers
	// based on draft status and ReviewersAddWhen setting.
	opts.Assignees = mergeConfiguredValues(opts.Assignees, opts.ConfiguredAssignees)
}

// effectiveLabels returns the labels to add to a change request.
// Flag-specified labels are always included.
// Configured labels are included based on the LabelsAddWhen setting
// and whether the change request is being created or updated.
func effectiveLabels(opts *Options, isCreate bool) []string {
	// If addWhen is "create" and the CR already exists,
	// skip configured labels.
	if opts.LabelsAddWhen == LabelsAddWhenCreate && !isCreate {
		return opts.Labels
	}
	return mergeConfiguredValues(opts.Labels, opts.ConfiguredLabels)
}

// effectiveReviewers returns the reviewers to add to a change request.
// Flag-specified reviewers are always included.
// Configured reviewers are included based on the ReviewersAddWhen setting
// and the draft status of the change request.
func effectiveReviewers(opts *Options, isDraft bool) []string {
	// If addWhen is "ready" and the CR is a draft,
	// skip configured reviewers.
	if opts.ReviewersAddWhen == ReviewersAddWhenReady && isDraft {
		return opts.Reviewers
	}
	return mergeConfiguredValues(opts.Reviewers, opts.ConfiguredReviewers)
}

// NavCommentSync specifies the scope of navigation comment updates.
type NavCommentSync int

const (
	// NavCommentSyncBranch updates navigation comments
	// only for branches that are being submitted.
	//
	// This is the default.
	NavCommentSyncBranch NavCommentSync = iota

	// NavCommentSyncDownstack updates navigation comments
	// for all submitted branches and their downstack branches.
	NavCommentSyncDownstack
)

var _ encoding.TextUnmarshaler = (*NavCommentSync)(nil)

// String returns the string representation of the NavCommentSync.
func (s NavCommentSync) String() string {
	switch s {
	case NavCommentSyncBranch:
		return "branch"
	case NavCommentSyncDownstack:
		return "downstack"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes a NavCommentSync from text.
// It supports "branch" and "downstack" values.
func (s *NavCommentSync) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "branch":
		*s = NavCommentSyncBranch
	case "downstack":
		*s = NavCommentSyncDownstack
	default:
		return fmt.Errorf("invalid value %q: expected branch or downstack", bs)
	}
	return nil
}

// NavCommentDownstack specifies which downstack CRs
// to include in navigation comments.
type NavCommentDownstack int

const (
	// NavCommentDownstackAll includes all downstack CRs
	// (both open and merged).
	//
	// This is the default.
	NavCommentDownstackAll NavCommentDownstack = iota

	// NavCommentDownstackOpen includes only open downstack CRs,
	// excluding merged ones.
	NavCommentDownstackOpen
)

var _ encoding.TextUnmarshaler = (*NavCommentDownstack)(nil)

// String returns the string representation of the NavCommentDownstack.
func (d NavCommentDownstack) String() string {
	switch d {
	case NavCommentDownstackAll:
		return "all"
	case NavCommentDownstackOpen:
		return "open"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes NavCommentDownstack from text.
func (d *NavCommentDownstack) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "all":
		*d = NavCommentDownstackAll
	case "open":
		*d = NavCommentDownstackOpen
	default:
		return fmt.Errorf("invalid value %q: expected all or open", bs)
	}
	return nil
}

// NavCommentTrunkLink specifies whether navigation comments include a link
// comparing the branch against trunk, and which CRs in a stack receive it.
type NavCommentTrunkLink int

const (
	// NavCommentTrunkLinkOff disables the trunk comparison link.
	//
	// This is the default.
	NavCommentTrunkLinkOff NavCommentTrunkLink = iota

	// NavCommentTrunkLinkTop adds the trunk comparison link
	// only to the topmost CRs of a stack
	// (those with nothing else stacked on top of them).
	// On the tip of a stack, this shows the whole stack's diff
	// against trunk in a single comparison.
	NavCommentTrunkLinkTop

	// NavCommentTrunkLinkAll adds the trunk comparison link
	// to every CR in a stack that is stacked on top of another CR.
	NavCommentTrunkLinkAll
)

var _ encoding.TextUnmarshaler = (*NavCommentTrunkLink)(nil)

// String returns the string representation of the NavCommentTrunkLink.
func (t NavCommentTrunkLink) String() string {
	switch t {
	case NavCommentTrunkLinkOff:
		return "false"
	case NavCommentTrunkLinkTop:
		return "top"
	case NavCommentTrunkLinkAll:
		return "all"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes a NavCommentTrunkLink from text.
// It supports "false", "top", and "all" values.
// "true" is accepted as an alias for "top".
func (t *NavCommentTrunkLink) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "false", "0", "no":
		*t = NavCommentTrunkLinkOff
	case "true", "top":
		*t = NavCommentTrunkLinkTop
	case "all":
		*t = NavCommentTrunkLinkAll
	default:
		return fmt.Errorf("invalid value %q: expected false, top, or all", bs)
	}
	return nil
}

// ReviewersAddWhen specifies when configured reviewers
// should be added to change requests.
type ReviewersAddWhen int

const (
	// ReviewersAddWhenAlways adds configured reviewers
	// to all change requests regardless of draft status.
	//
	// This is the default.
	ReviewersAddWhenAlways ReviewersAddWhen = iota

	// ReviewersAddWhenReady adds configured reviewers
	// only when the change request is not a draft.
	ReviewersAddWhenReady
)

var _ encoding.TextUnmarshaler = (*ReviewersAddWhen)(nil)

// String returns the string representation of the ReviewersAddWhen.
func (r ReviewersAddWhen) String() string {
	switch r {
	case ReviewersAddWhenAlways:
		return "always"
	case ReviewersAddWhenReady:
		return "ready"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes ReviewersAddWhen from text.
// It supports "always" and "ready" values.
func (r *ReviewersAddWhen) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "always":
		*r = ReviewersAddWhenAlways
	case "ready":
		*r = ReviewersAddWhenReady
	default:
		return fmt.Errorf("invalid value %q: expected always or ready", bs)
	}
	return nil
}

// LabelsAddWhen specifies when configured labels
// should be added to change requests.
type LabelsAddWhen int

const (
	// LabelsAddWhenAlways adds configured labels
	// to all change requests on every submit.
	//
	// This is the default.
	LabelsAddWhenAlways LabelsAddWhen = iota

	// LabelsAddWhenCreate adds configured labels
	// only when creating a new change request.
	LabelsAddWhenCreate
)

var _ encoding.TextUnmarshaler = (*LabelsAddWhen)(nil)

// String returns the string representation of the LabelsAddWhen.
func (l LabelsAddWhen) String() string {
	switch l {
	case LabelsAddWhenAlways:
		return "always"
	case LabelsAddWhenCreate:
		return "create"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes LabelsAddWhen from text.
// It supports "always" and "create" values.
func (l *LabelsAddWhen) UnmarshalText(bs []byte) error {
	switch string(bs) {
	case "always":
		*l = LabelsAddWhenAlways
	case "create":
		*l = LabelsAddWhenCreate
	default:
		return fmt.Errorf("invalid value %q: expected always or create", bs)
	}
	return nil
}
