package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
)

// Bitbucket Data Center pull request states.
const (
	statePROpen     = "OPEN"
	statePRMerged   = "MERGED"
	statePRDeclined = "DECLINED"
)

// serverStateFromAPI maps a Bitbucket Data Center pull request state to a
// [forge.ChangeState]; unknown values fall back to [forge.ChangeOpen].
//
// It is deliberately separate from Bitbucket Cloud's stateFromAPI:
// Cloud additionally maps the DRAFT and SUPERSEDED states,
// which Data Center does not have.
func serverStateFromAPI(state string) forge.ChangeState {
	switch state {
	case statePRMerged:
		return forge.ChangeMerged
	case statePRDeclined:
		return forge.ChangeClosed
	case statePROpen:
		return forge.ChangeOpen
	default:
		return forge.ChangeOpen
	}
}

// serverStateToAPI maps a forge change-state filter to a Bitbucket Data Center
// pull request "state" query value; the zero state means all states.
//
// It is deliberately separate from Bitbucket Cloud's stateToAPI:
// an unset filter defaults to "ALL" here, but to "OPEN" for Cloud.
func serverStateToAPI(state forge.ChangeState) string {
	switch state {
	case forge.ChangeMerged:
		return statePRMerged
	case forge.ChangeClosed:
		return statePRDeclined
	case forge.ChangeOpen:
		return statePROpen
	default:
		return "ALL"
	}
}

// CreateChange creates a new pull request.
func (g *Gateway) CreateChange(
	ctx context.Context,
	req bitbucket.CreateChangeRequest,
) (*bitbucket.PullRequest, error) {
	if err := g.ensureSameRepository(req.PushRepository); err != nil {
		return nil, err
	}

	reviewers, err := g.buildReviewers(ctx, req.Reviewers, req.Head, req.Base)
	if err != nil {
		return nil, err
	}

	apiReq := g.buildCreatePRRequest(req, reviewers)

	// Draft PRs require Data Center 8.18+. If the version is readable and too
	// old, downgrade; if unreadable, try draft:true and verify afterward.
	verifyDraft := false
	if req.Draft {
		switch supported, known, version := g.draftSupport(ctx); {
		case known && !supported:
			g.log.Warn(
				"Bitbucket Data Center < 8.18 does not support draft pull requests; "+
					"creating a regular pull request",
				"serverVersion", version,
			)
			apiReq.Draft = false
		case !known:
			verifyDraft = true
		}
	}

	pr, _, err := g.client.PullRequestCreate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, apiReq,
	)
	if err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}

	if verifyDraft && !pr.Draft {
		g.log.Warn(
			"Server created a non-draft pull request; " +
				"the draft flag may be unsupported on this Bitbucket Data Center version",
		)
	}

	change := g.toPullRequest(pr)
	g.log.Debug("Created pull request", "pr", pr.ID, "url", change.URL)
	return change, nil
}

// ensureSameRepository errors if the head branch lives in another repository;
// cross-repository (fork) pull requests are not yet supported.
func (g *Gateway) ensureSameRepository(pushRepo forge.RepositoryID) error {
	if pushRepo == nil {
		return nil
	}
	if pushRepo.String() == g.repoID.String() {
		return nil
	}
	return fmt.Errorf(
		"cross-repository pull requests are not yet supported "+
			"by the Bitbucket Data Center forge: head branch is in %q, not %q",
		pushRepo.String(), g.repoID.String(),
	)
}

// buildReviewers resolves the pull request's reviewers — the project's default
// reviewers followed by the requested usernames — into the thin-client shape,
// deduplicated by username with the authenticated user dropped (Data Center
// rejects self-review).
//
// A current-user lookup failure is fatal when explicit reviewers were
// requested, but only drops the best-effort default reviewers otherwise.
func (g *Gateway) buildReviewers(
	ctx context.Context,
	usernames []string,
	head string,
	base string,
) ([]CreateReviewer, error) {
	// Default reviewers are best-effort; they may be empty.
	defaults := g.defaultReviewers(ctx, head, base)
	if len(defaults) == 0 && len(usernames) == 0 {
		return nil, nil
	}

	// Drop self; a lookup failure is fatal only when explicit reviewers exist.
	var self string
	if user, _, err := g.client.CurrentUser(ctx); err != nil {
		if len(usernames) > 0 {
			return nil, fmt.Errorf("identify current user: %w", err)
		}
		g.log.Debug("Could not identify current user; skipping default reviewers", "error", err)
		return nil, nil
	} else if user != nil {
		self = user.Name
	}

	// Defaults first, then explicit usernames; dedup keeps first-seen order.
	return newReviewerList(self, defaults, usernames), nil
}

// defaultReviewers returns the usernames of the project's default (required)
// reviewers for a head->base pull request.
//
// It is best-effort: any failure is logged at debug level and yields nil,
// never failing the create. No configured default reviewers is the common
// case, not an error.
func (g *Gateway) defaultReviewers(ctx context.Context, head, base string) []string {
	repoID, err := g.numericRepoID(ctx)
	if err != nil {
		g.log.Debug("Could not resolve repository ID; skipping default reviewers", "error", err)
		return nil
	}

	// Use the same fully qualified refs as creation (see createRef).
	reviewers, _, err := g.client.DefaultReviewers(
		ctx, g.repoID.restProjectKey(), g.repoID.slug,
		repoID, repoID,
		"refs/heads/"+head, "refs/heads/"+base,
	)
	if err != nil {
		g.log.Debug("Could not fetch default reviewers; proceeding with explicit reviewers only", "error", err)
		return nil
	}

	names := make([]string, 0, len(reviewers))
	for _, reviewer := range reviewers {
		if reviewer.Name != "" {
			names = append(names, reviewer.Name)
		}
	}
	return names
}

// buildCreatePRRequest assembles the thin-client create request from req.
func (g *Gateway) buildCreatePRRequest(
	req bitbucket.CreateChangeRequest,
	reviewers []CreateReviewer,
) PullRequestCreateRequest {
	return PullRequestCreateRequest{
		Title:       req.Subject,
		Description: req.Body,
		FromRef:     g.createRef(req.Head),
		ToRef:       g.createRef(req.Base),
		Reviewers:   reviewers,
		Draft:       req.Draft,
	}
}

// createRef builds a pull request ref for branch in this repository, with the
// fully qualified "refs/heads/{branch}" ref ID.
func (g *Gateway) createRef(branch string) CreateRef {
	return CreateRef{
		ID: "refs/heads/" + branch,
		Repository: CreateRefRepository{
			Slug: g.repoID.slug,
			Project: CreateRefProject{
				Key: g.repoID.restProjectKey(),
			},
		},
	}
}

// GetChange retrieves a pull request by number.
func (g *Gateway) GetChange(
	ctx context.Context,
	number int64,
) (*bitbucket.PullRequest, error) {
	pr, _, err := g.client.PullRequestGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
	)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	return g.toPullRequest(pr), nil
}

// ChangeMergeability reports whether a pull request can be merged.
func (g *Gateway) ChangeMergeability(
	ctx context.Context,
	number int64,
) (forge.ChangeMergeability, error) {
	status, _, err := g.client.PullRequestCanMerge(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
	)
	if err != nil {
		return forge.ChangeMergeability{}, fmt.Errorf("get mergeability: %w", err)
	}
	return mergeabilityFromAPI(status), nil
}

func mergeabilityFromAPI(status *MergeStatus) forge.ChangeMergeability {
	result := forge.ChangeMergeability{
		Reason: forge.ChangeMergeabilityReasonUnknown,
	}
	switch {
	case status == nil:
		result.State = forge.ChangeMergeabilityUnknown
	case status.CanMerge:
		result.State = forge.ChangeMergeabilityReady
	case status.Conflicted || status.Outcome == "CONFLICTED":
		result.State = forge.ChangeMergeabilityBlocked
		result.Reason = forge.ChangeMergeabilityReasonConflicts
	default:
		result.State = forge.ChangeMergeabilityBlocked
	}
	return result
}

// FindChangesByBranch lists pull requests
// whose source branch has the given name,
// returning up to opts.Limit results (zero means no limit).
//
// Cross-repository (fork) pull requests are unsupported,
// so a different push repository cannot match.
func (g *Gateway) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts bitbucket.FindChangesOptions,
) ([]*bitbucket.PullRequest, error) {
	if opts.PushRepository != nil && opts.PushRepository.String() != g.repoID.String() {
		return nil, nil
	}

	req := PullRequestListRequest{
		At:        "refs/heads/" + branch,
		Direction: "OUTGOING",
		State:     serverStateToAPI(opts.State),
	}

	var changes []*bitbucket.PullRequest
	for pr, err := range g.client.PullRequestList(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, req,
	) {
		if err != nil {
			return nil, fmt.Errorf("list pull requests: %w", err)
		}

		changes = append(changes, g.toPullRequest(&pr))
		if opts.Limit > 0 && len(changes) >= opts.Limit {
			break
		}
	}
	return changes, nil
}

// UpdateChange modifies an existing pull request.
//
// Data Center replaces the mutable fields wholesale under an
// optimistic-locking version, so the pull request is fetched first and its
// current title, description, and reviewers are carried into the update.
// A stale version ([ErrConflict]) triggers one refetch
// and retry.
func (g *Gateway) UpdateChange(
	ctx context.Context,
	number int64,
	update bitbucket.ChangeUpdate,
) error {
	pr, _, err := g.client.PullRequestGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
	)
	if err != nil {
		return fmt.Errorf("get pull request: %w", err)
	}

	_, _, err = g.client.PullRequestUpdate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
		g.buildUpdateRequest(pr, update),
	)
	if errors.Is(err, ErrConflict) {
		g.log.Debug("Pull request version conflict; refetching and retrying", "pr", number)
		pr, _, err = g.client.PullRequestGet(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
		)
		if err != nil {
			return fmt.Errorf("refetch pull request: %w", err)
		}
		// Rebuild from the refetched pull request
		// so the conflicting edit's fields are not overwritten.
		_, _, err = g.client.PullRequestUpdate(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
			g.buildUpdateRequest(pr, update),
		)
	}
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}

	g.log.Debug("Updated pull request", "pr", number)
	return nil
}

// SetChangeDraft reports that Bitbucket Data Center cannot change
// the draft status of an existing pull request.
func (*Gateway) SetChangeDraft(context.Context, int64, bool) error {
	return bitbucket.ErrUnsupported
}

// buildUpdateRequest assembles the thin-client update request from the
// current pull request and the requested update. The update replaces the
// mutable fields wholesale, so the current title, description, and reviewers
// are always carried over; requested reviewers are appended, deduplicated by
// username, with the author dropped (Data Center rejects self-review).
func (g *Gateway) buildUpdateRequest(
	pr *PullRequest,
	update bitbucket.ChangeUpdate,
) PullRequestUpdateRequest {
	req := PullRequestUpdateRequest{
		Version:     pr.Version,
		Title:       pr.Title,
		Description: &pr.Description,
		Reviewers: newReviewerList(
			pr.Author.User.Name,
			reviewerNames(pr.Reviewers),
			update.AddReviewers,
		),
	}

	if update.Base != "" {
		req.ToRef = &UpdateRef{ID: "refs/heads/" + update.Base}
	}

	return req
}

// MergeChange merges an open pull request into its base branch.
//
// method selects a Data Center merge strategy; an unset or unknown method
// lets the server use the repository default. The merge is guarded by an
// optimistic-locking version, so a conflict triggers one refetch and retry.
func (g *Gateway) MergeChange(
	ctx context.Context,
	number int64,
	method forge.MergeMethod,
) error {
	// Best-effort pre-merge probe: block early on a clear "cannot merge".
	// Any probe error falls through to the merge attempt.
	if status, _, err := g.client.PullRequestCanMerge(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
	); err != nil {
		g.log.Debug("Pre-merge check failed; proceeding with merge attempt", "pr", number, "error", err)
	} else if status != nil && !status.CanMerge {
		return fmt.Errorf("%w: %s", bitbucket.ErrMergeBlocked, formatVetoes(status))
	}

	version, err := g.currentPRVersion(ctx, number)
	if err != nil {
		return err
	}

	req := PullRequestMergeRequest{
		StrategyID: g.mergeStrategyID(method),
	}
	_, _, err = g.client.PullRequestMerge(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number, version, req,
	)

	if errors.Is(err, ErrConflict) {
		g.log.Debug("Pull request version conflict; refetching and retrying", "pr", number)
		version, err = g.currentPRVersion(ctx, number)
		if err != nil {
			return err
		}
		_, _, err = g.client.PullRequestMerge(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, number, version, req,
		)
	}

	if err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	g.log.Debug("Merged pull request", "pr", number)
	return nil
}

// currentPRVersion fetches a pull request's current optimistic-locking version.
func (g *Gateway) currentPRVersion(ctx context.Context, number int64) (int, error) {
	pr, _, err := g.client.PullRequestGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, number,
	)
	if err != nil {
		return 0, fmt.Errorf("get pull request: %w", err)
	}
	return pr.Version, nil
}

// mergeStrategyID maps a forge merge method to a Data Center merge strategy ID.
// An unset or unrecognized method returns "", so the server uses its default.
func (g *Gateway) mergeStrategyID(method forge.MergeMethod) string {
	switch method {
	case forge.MergeMethodMerge:
		return "no-ff"
	case forge.MergeMethodSquash:
		return "squash"
	case forge.MergeMethodRebase:
		return "rebase-no-ff"
	case forge.MergeMethodDefault:
		return ""
	default:
		g.log.Warn(
			"Unsupported merge method; using repository default",
			"method", method,
		)
		return ""
	}
}

// toPullRequest maps a Data Center pull request
// to the product-neutral shape.
//
// The URL prefers the server's self link
// and falls back to one built from the repository ID.
func (g *Gateway) toPullRequest(pr *PullRequest) *bitbucket.PullRequest {
	return &bitbucket.PullRequest{
		Number:    pr.ID,
		URL:       changeURL(pr, g.repoID, pr.ID),
		State:     serverStateFromAPI(pr.State),
		Subject:   pr.Title,
		BaseName:  pr.ToRef.DisplayID,
		HeadHash:  git.Hash(pr.FromRef.LatestCommit),
		Draft:     pr.Draft,
		Reviewers: reviewerNames(pr.Reviewers),
	}
}

// changeURL returns the web URL for a pull request, preferring the
// server's self link and falling back to one built from the repository ID.
func changeURL(pr *PullRequest, rid *serverRepositoryID, number int64) string {
	if len(pr.Links.Self) > 0 && pr.Links.Self[0].Href != "" {
		return pr.Links.Self[0].Href
	}
	return rid.ChangeURL(number)
}

// newReviewerList builds the thin-client reviewer list from the name lists
// in order, dropping empty names, duplicates, and exclude (Data Center
// rejects self-review).
func newReviewerList(
	exclude string,
	nameLists ...[]string,
) []CreateReviewer {
	var reviewers []CreateReviewer
	seen := make(map[string]struct{})
	for _, names := range nameLists {
		for _, name := range names {
			if name == "" || name == exclude {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			reviewers = append(reviewers, CreateReviewer{
				User: CreateReviewerUser{Name: name},
			})
		}
	}
	return reviewers
}

// reviewerNames extracts the usernames of the given reviewers.
func reviewerNames(reviewers []Reviewer) []string {
	var names []string
	for _, rev := range reviewers {
		if rev.User.Name != "" {
			names = append(names, rev.User.Name)
		}
	}
	return names
}

// formatVetoes renders a human-readable reason a pull request cannot be merged,
// joining each veto's message with "; ". With no usable vetoes it reports a
// merge conflict (when indicated) or a generic reason. Veto text is
// server-localized, so it is only displayed, never parsed.
func formatVetoes(s *MergeStatus) string {
	msgs := make([]string, 0, len(s.Vetoes))
	for _, v := range s.Vetoes {
		switch {
		case v.SummaryMessage != "":
			msgs = append(msgs, v.SummaryMessage)
		case v.DetailedMessage != "":
			msgs = append(msgs, v.DetailedMessage)
		}
	}
	if len(msgs) == 0 {
		if s.Conflicted || s.Outcome == "CONFLICTED" {
			return "the pull request has merge conflicts"
		}
		return "the server reported it is not mergeable"
	}
	return strings.Join(msgs, "; ")
}
