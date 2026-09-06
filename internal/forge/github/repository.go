package github

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog"
)

//go:generate mockgen -destination=mocks_test.go -package=github -write_package_comment=false -typed -mock_names=githubGateway=MockGithubGateway . githubGateway

// githubGateway is the GitHub API boundary consumed by Repository.
type githubGateway interface {
	AddComment(context.Context, github.ID, string) (*github.AddedComment, error)
	AddPullRequestReview(context.Context, *github.AddPullRequestReviewInput) (*github.AddedPullRequestReview, error)
	AddPullRequestReviewThread(context.Context, *github.AddPullRequestReviewThreadInput) (*github.AddedPullRequestReviewThread, error)
	AddPullRequestReviewThreadReply(context.Context, *github.AddPullRequestReviewThreadReplyInput) (*github.AddedPullRequestReviewComment, error)
	AddPullRequestsToStack(context.Context, *github.AddPullRequestsToStackInput) error
	AddPullRequestMetadata(context.Context, *github.PullRequestMetadataInput) error
	AsyncMergeResult(context.Context, string, string, int, string) (*github.AsyncMergeResult, error)
	ChangeStatuses(context.Context, []github.ID) ([]*github.ChangeStatus, error)
	ChangeTemplates(context.Context, string, string) ([]*github.ChangeTemplate, error)
	CheckPullRequestStacks(context.Context, string, string) error
	ClosePullRequest(context.Context, github.ID) error
	ConvertPullRequestToDraft(context.Context, github.ID) error
	CreatePullRequestStack(context.Context, *github.CreatePullRequestStackInput) error
	CreateLabel(context.Context, github.ID, string, string) (github.ID, error)
	CreatePullRequest(context.Context, *github.CreatePullRequestInput) (*github.CreatedPullRequest, error)
	DeleteIssueComment(context.Context, github.ID) error
	DeleteLabel(context.Context, github.ID) error
	FindPullRequests(context.Context, string, string, string, int, []github.PullRequestState) ([]*github.PullRequest, error)
	FindPullRequestsByBranches(context.Context, *github.FindPullRequestsByBranchesRequest) ([][]*github.PullRequestBranchMatch, error)
	IdentityIDs(context.Context, []string, []github.TeamName) ([]github.ID, []github.ID, error)
	LabelIDs(context.Context, string, string, []string) ([]github.ID, error)
	MarkPullRequestReadyForReview(context.Context, github.ID) error
	MergePullRequest(context.Context, *github.MergePullRequestInput) error
	MergePullRequestAsync(context.Context, *github.MergePullRequestAsyncInput) (*github.AsyncMergeResult, error)
	PullRequest(context.Context, string, string, int) (*github.PullRequest, error)
	PullRequestsForMergeRange(context.Context, string, string, []int) ([]*github.MergeRangePullRequest, error)
	PullRequestsForStackUpdate(context.Context, string, string, []int) ([]*github.StackUpdatePullRequest, error)
	PullRequestComments(context.Context, github.ID, *github.PaginationOptions) iter.Seq2[*github.Comment, error]
	PullRequestID(context.Context, string, string, int) (github.ID, error)
	PullRequestMergeability(context.Context, github.ID) (*github.Mergeability, error)
	PullRequestLatestOpinionatedReviews(context.Context, github.ID, *github.PaginationOptions) iter.Seq2[*github.PullRequestLatestOpinionatedReview, error]
	PullRequestReviewThreads(context.Context, github.ID, *github.PaginationOptions) iter.Seq2[*github.PullRequestReviewThread, error]
	PullRequestReviewThreadCounts(context.Context, []github.ID, *github.PaginationOptions) ([]*github.ReviewThreadCounts, error)
	RefExists(context.Context, string, string, string) (bool, error)
	RepositoryID(context.Context, string, string) (github.ID, error)
	ResolveReviewThread(context.Context, github.ID) error
	StatusChecks(context.Context, github.ID, *github.PaginationOptions) iter.Seq2[github.StatusCheck, error]
	SubmitPullRequestReview(context.Context, *github.SubmitPullRequestReviewInput) error
	UnresolveReviewThread(context.Context, github.ID) error
	UpdateIssueComment(context.Context, github.ID, string) error
	UpdatePullRequest(context.Context, *github.UpdatePullRequestInput) error
	UpdatePullRequestReviewComment(context.Context, github.ID, string) error
	UnstackPullRequestStack(context.Context, *github.UnstackPullRequestStackInput) (*github.UnstackPullRequestStackResult, error)
}

var _ githubGateway = (*github.Gateway)(nil)

// Repository is a GitHub repository.
type Repository struct {
	owner, repo string
	repoID      github.ID
	log         *silog.Logger
	gateway     githubGateway
	forge       *Forge

	identityIDsMu sync.RWMutex // guards userIDsCache and teamIDsCache
	// userIDsCache caches successful login lookups for this repository.
	//
	// Pull request metadata operations can resolve the same login
	// through reviewers, assignees, or follow-up edits in one command.
	userIDsCache map[string]github.ID

	// teamIDsCache caches successful organization and team slug lookups.
	teamIDsCache map[github.TeamName]github.ID
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	forge *Forge,
	owner, repo string,
	log *silog.Logger,
	gateway githubGateway,
	repoID github.ID,
) (*Repository, error) {
	log = log.With("repo", fmt.Sprintf("%s/%s", owner, repo))
	if repoID == "" {
		var err error
		repositoryID, err := gateway.RepositoryID(ctx, owner, repo)
		if err != nil {
			return nil, fmt.Errorf("get repository ID: %w", err)
		}
		repoID = repositoryID
	}

	return &Repository{
		owner:        owner,
		repo:         repo,
		log:          log,
		gateway:      gateway,
		repoID:       repoID,
		forge:        forge,
		userIDsCache: make(map[string]github.ID),
		teamIDsCache: make(map[github.TeamName]github.ID),
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

var _ forge.WithComparisonURL = (*Repository)(nil)

// ComparisonURL returns a URL for a comparison on GitHub.
// See GitHub's [comparing commits documentation].
//
// [comparing commits documentation]: https://docs.github.com/en/pull-requests/committing-changes-to-your-project/viewing-and-comparing-commits/comparing-commits
func (r *Repository) ComparisonURL(req forge.ComparisonRequest) string {
	head := req.HeadURLEncoded()
	if req.HeadRepository != nil {
		headRepo := mustRepositoryID(req.HeadRepository)
		if headRepo.owner != r.owner || headRepo.name != r.repo {
			// GitHub qualifies a cross-fork head with its owner.
			head = url.PathEscape(headRepo.owner) + ":" + head
		}
	}
	return fmt.Sprintf("%s/%s/%s/compare/%s...%s",
		r.forge.URL(), r.owner, r.repo, req.BaseURLEncoded(), head)
}
