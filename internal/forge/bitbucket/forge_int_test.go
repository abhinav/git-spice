package bitbucket

import (
	"net/http"

	"go.abhg.dev/gs/internal/silog"
)

// NewRepositoryForTest creates a Repository for integration testing.
// It accepts a custom HTTP client for VCR recording/replay.
func NewRepositoryForTest(
	forge *Forge,
	url, workspace, repo string,
	log *silog.Logger,
	httpClient *http.Client,
	token *AuthenticationToken,
) *Repository {
	client := newClientWithHTTP(forge.APIURL(), token, log, httpClient)
	return newRepository(forge, url, workspace, repo, log, client)
}
