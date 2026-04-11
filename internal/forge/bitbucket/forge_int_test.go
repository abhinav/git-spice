package bitbucket

import (
	"net/http"

	"go.abhg.dev/gs/internal/gateway/bitbucket"
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
	tokenSource, err := newGatewayTokenSource(token)
	if err != nil {
		panic(err)
	}

	client, err := bitbucket.NewClient(tokenSource, &bitbucket.ClientOptions{
		BaseURL:    forge.APIURL(),
		HTTPClient: httpClient,
	})
	if err != nil {
		panic(err)
	}

	return newRepository(forge, url, workspace, repo, log, client, token, httpClient)
}
