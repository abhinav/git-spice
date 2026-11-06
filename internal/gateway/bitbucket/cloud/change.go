package cloud

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
)

// Bitbucket Cloud pull request states.
const (
	stateOpen       = "OPEN"
	stateMerged     = "MERGED"
	stateDeclined   = "DECLINED"
	stateSuperseded = "SUPERSEDED"
)

// stateFromAPI maps a Bitbucket Cloud pull request state
// to a [forge.ChangeState];
// unknown values fall back to [forge.ChangeOpen].
func stateFromAPI(state string) forge.ChangeState {
	switch state {
	case stateOpen, "DRAFT":
		return forge.ChangeOpen
	case stateMerged:
		return forge.ChangeMerged
	case stateDeclined, stateSuperseded:
		return forge.ChangeClosed
	default:
		return forge.ChangeOpen
	}
}

// stateToAPI maps a forge change-state filter
// to a Bitbucket Cloud pull request "state" query value.
func stateToAPI(state forge.ChangeState) string {
	switch state {
	case forge.ChangeOpen:
		return stateOpen
	case forge.ChangeMerged:
		return stateMerged
	case forge.ChangeClosed:
		return stateDeclined
	default:
		return stateOpen
	}
}

// CreateChange creates a new pull request.
func (g *Gateway) CreateChange(
	ctx context.Context,
	req bitbucket.CreateChangeRequest,
) (*bitbucket.PullRequest, error) {
	reviewers, err := g.resolveReviewerUUIDs(ctx, req.Reviewers)
	if err != nil {
		return nil, fmt.Errorf("resolve reviewers: %w", err)
	}

	apiReq := &PullRequestCreateRequest{
		Title: req.Subject,
		Source: BranchRef{
			Branch: Branch{Name: req.Head},
		},
		Destination: BranchRef{
			Branch: Branch{Name: req.Base},
		},
		Draft: req.Draft,
	}
	if req.PushRepository != nil {
		apiReq.Source.Repository = &RepositoryRef{
			FullName: req.PushRepository.String(),
		}
	}
	if req.Body != "" {
		apiReq.Description = req.Body
	}
	if len(reviewers) > 0 {
		apiReq.Reviewers = make([]Reviewer, 0, len(reviewers))
		for _, reviewer := range reviewers {
			apiReq.Reviewers = append(apiReq.Reviewers, Reviewer{
				UUID: reviewer,
			})
		}
	}

	pr, _, err := g.client.PullRequestCreate(ctx, g.workspace, g.repo, apiReq)
	if err != nil {
		if errors.Is(err, ErrDestinationBranchNotFound) {
			return nil, fmt.Errorf(
				"create pull request: %w", forge.ErrUnsubmittedBase,
			)
		}
		return nil, fmt.Errorf("create pull request: %w", err)
	}

	g.log.Debug("Created pull request", "pr", pr.ID, "url", pr.Links.HTML.Href)
	return g.toPullRequest(pr), nil
}

// GetChange retrieves a pull request by number.
func (g *Gateway) GetChange(
	ctx context.Context,
	number int64,
) (*bitbucket.PullRequest, error) {
	pr, err := g.getPullRequest(ctx, number)
	if err != nil {
		return nil, err
	}
	return g.toPullRequest(pr), nil
}

// FindChangesByBranch lists pull requests
// whose source branch has the given name.
func (g *Gateway) FindChangesByBranch(
	ctx context.Context,
	branch string,
	opts bitbucket.FindChangesOptions,
) ([]*bitbucket.PullRequest, error) {
	query := fmt.Sprintf(`source.branch.name="%s"`, branch)
	sourceRepository := g.workspace + "/" + g.repo
	if opts.PushRepository != nil {
		sourceRepository = opts.PushRepository.String()
	}
	query += fmt.Sprintf(
		` AND source.repository.full_name="%s"`, sourceRepository,
	)
	if opts.State != 0 {
		query += fmt.Sprintf(` AND state="%s"`, stateToAPI(opts.State))
	}

	resp, _, err := g.client.PullRequestList(ctx, g.workspace, g.repo,
		&PullRequestListOptions{
			Query:   query,
			PageLen: opts.Limit,
			Fields:  []string{"+values.reviewers"},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}

	prs := make([]*bitbucket.PullRequest, len(resp.Values))
	for i := range resp.Values {
		prs[i] = g.toPullRequest(&resp.Values[i])
	}
	return prs, nil
}

// UpdateChange modifies an existing pull request.
//
// Bitbucket Cloud replaces the entire pull request resource on PUT,
// so each kind of update sends its own minimal PUT:
// a base change updates only the destination branch,
// while adding reviewers re-sends title, description, and reviewers
// to preserve them.
func (g *Gateway) UpdateChange(
	ctx context.Context,
	number int64,
	update bitbucket.ChangeUpdate,
) error {
	if update.Base != "" {
		err := g.updatePullRequest(ctx, number,
			&PullRequestUpdateRequest{
				Destination: &BranchRef{
					Branch: Branch{Name: update.Base},
				},
			})
		if err != nil {
			return err
		}
	}

	if len(update.AddReviewers) > 0 {
		if err := g.addReviewers(ctx, number, update.AddReviewers); err != nil {
			return err
		}
	}

	return nil
}

// addReviewers requests reviews from the given users
// in addition to the already requested reviewers.
func (g *Gateway) addReviewers(
	ctx context.Context,
	number int64,
	reviewers []string,
) error {
	// First get the current PR to preserve existing reviewers.
	pr, err := g.getPullRequest(ctx, number)
	if err != nil {
		return fmt.Errorf("get current PR: %w", err)
	}

	// Resolve new reviewer UUIDs.
	newReviewers, err := g.resolveReviewerUUIDs(ctx, reviewers)
	if err != nil {
		return fmt.Errorf("resolve reviewers: %w", err)
	}

	return g.updatePullRequest(ctx, number, &PullRequestUpdateRequest{
		Title:       &pr.Title,       // required by Bitbucket PUT
		Description: &pr.Description, // preserve existing description
		Reviewers:   mergeReviewers(pr.Reviewers, newReviewers),
	})
}

// SetChangeDraft changes the draft status of a pull request.
// Bitbucket Cloud supports toggling it with a single-field PUT.
func (g *Gateway) SetChangeDraft(
	ctx context.Context,
	number int64,
	draft bool,
) error {
	return g.updatePullRequest(ctx, number, &PullRequestUpdateRequest{
		Draft: &draft,
	})
}

// MergeChange merges a pull request using the given method.
//
// Bitbucket Cloud does not support expected-SHA assertions.
func (g *Gateway) MergeChange(
	ctx context.Context,
	number int64,
	method forge.MergeMethod,
) error {
	var strategy string
	switch method {
	case forge.MergeMethodDefault:
	case forge.MergeMethodMerge:
		strategy = "merge_commit"
	case forge.MergeMethodSquash:
		strategy = "squash"
	case forge.MergeMethodRebase:
		strategy = "rebase_merge"
	default:
		g.log.Warn(
			"Unsupported merge method; using forge default",
			"method", method,
		)
	}

	if _, _, err := g.client.PullRequestMerge(
		ctx, g.workspace, g.repo, number,
		&PullRequestMergeRequest{Strategy: strategy},
	); err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	g.log.Debug("Merged pull request", "pr", number)
	return nil
}

// getPullRequest fetches the raw pull request resource by number.
func (g *Gateway) getPullRequest(
	ctx context.Context,
	number int64,
) (*PullRequest, error) {
	pr, _, err := g.client.PullRequestGet(ctx, g.workspace, g.repo, number)
	if err != nil {
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	return pr, nil
}

// updatePullRequest sends a single PUT
// carrying the given partial pull request update.
func (g *Gateway) updatePullRequest(
	ctx context.Context,
	number int64,
	req *PullRequestUpdateRequest,
) error {
	_, _, err := g.client.PullRequestUpdate(ctx, g.workspace, g.repo, number, req)
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}
	g.log.Debug("Updated pull request", "pr", number)
	return nil
}

// toPullRequest converts a Bitbucket Cloud pull request
// into its product-neutral representation.
func (g *Gateway) toPullRequest(pr *PullRequest) *bitbucket.PullRequest {
	var headHash git.Hash
	if pr.Source.Commit != nil {
		headHash = git.Hash(pr.Source.Commit.Hash)
	}
	if headHash == "" && pr.MergeCommit != nil {
		headHash = git.Hash(pr.MergeCommit.Hash)
	}

	return &bitbucket.PullRequest{
		Number:    pr.ID,
		URL:       pr.Links.HTML.Href,
		State:     stateFromAPI(pr.State),
		Subject:   pr.Title,
		BaseName:  pr.Destination.Branch.Name,
		HeadHash:  headHash,
		Draft:     pr.Draft || pr.State == "DRAFT",
		Reviewers: extractUsernames(pr.Reviewers),
	}
}

// ChangeMergeability reports whether a pull request can be merged.
func (g *Gateway) ChangeMergeability(
	ctx context.Context,
	number int64,
) (forge.ChangeMergeability, error) {
	pr, err := g.getPullRequest(ctx, number)
	if err != nil {
		return forge.ChangeMergeability{}, err
	}
	return mergeabilityFromAPI(pr.Mergeable, pr.Queued), nil
}

func mergeabilityFromAPI(
	mergeable *bool,
	queued bool,
) forge.ChangeMergeability {
	// Bitbucket Cloud reports the mergeability decision,
	// but not the reason behind a blocked or waiting decision.
	result := forge.ChangeMergeability{
		Reason: forge.ChangeMergeabilityReasonUnknown,
	}
	switch {
	case mergeable == nil && queued:
		result.State = forge.ChangeMergeabilityWaiting
	case mergeable == nil:
		result.State = forge.ChangeMergeabilityUnknown
	case queued:
		result.State = forge.ChangeMergeabilityWaiting
	case *mergeable:
		result.State = forge.ChangeMergeabilityReady
	default:
		result.State = forge.ChangeMergeabilityBlocked
	}

	return result
}
