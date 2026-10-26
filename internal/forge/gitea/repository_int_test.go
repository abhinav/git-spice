package gitea

import (
	"context"
	"fmt"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// CloseChange closes a pull request without merging it.
// Used by integration tests.
func CloseChange(ctx context.Context, repo *Repository, id forge.ChangeID) error {
	pr := mustPR(id)
	state := "closed"
	_, _, err := repo.client.PullEdit(ctx, repo.owner, repo.repo, pr.Number, &giteagw.EditPullRequestOption{
		State: &state,
	})
	if err != nil {
		return fmt.Errorf("close pull request: %w", err)
	}
	return nil
}

// CommitStatusCreate sets the commit status for a SHA.
// Used by integration tests to simulate CI checks state.
func CommitStatusCreate(
	ctx context.Context,
	client *giteagw.Client,
	owner, repo, sha string,
	check forge.ChangeCheck,
) error {
	statusState := giteaStatusState(check.State)
	_, _, err := client.CommitStatusCreate(ctx, owner, repo, sha, &giteagw.CreateCommitStatusOption{
		State:   statusState,
		Context: check.Name,
	})
	if err != nil {
		return fmt.Errorf("create commit status: %w", err)
	}
	return nil
}

func giteaStatusState(state forge.ChangeCheckState) string {
	switch state {
	case forge.ChangeCheckPending:
		return giteagw.CommitStatusPending
	case forge.ChangeCheckPassed:
		return giteagw.CommitStatusSuccess
	case forge.ChangeCheckFailed:
		return giteagw.CommitStatusFailure
	default:
		return giteagw.CommitStatusFailure
	}
}

// NewGiteaClient creates a gateway client for integration tests.
func NewGiteaClient(token, baseURL string, httpClient *http.Client) (*giteagw.Client, error) {
	return giteagw.NewClient(
		giteagw.StaticTokenSource(giteagw.Token{
			Type:  giteagw.TokenTypeToken,
			Value: token,
		}),
		&giteagw.ClientOptions{
			BaseURL:    baseURL,
			HTTPClient: httpClient,
		},
	)
}
