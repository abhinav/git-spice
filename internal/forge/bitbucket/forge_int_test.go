package bitbucket

import (
	"context"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket/cloud"
	"go.abhg.dev/gs/internal/silog"
)

type testCloudGateway struct {
	*cloud.Gateway

	client          *cloud.Client
	token           *AuthenticationToken
	http            *http.Client
	apiURL          string
	workspace, repo string
}

// NewRepositoryForTest creates a Repository for integration testing.
// It accepts a custom HTTP client for VCR recording/replay.
func NewRepositoryForTest(
	forge *Forge,
	url, workspace, repo string,
	log *silog.Logger,
	httpClient *http.Client,
	token *AuthenticationToken,
) forge.Repository {
	var ctok *cloud.Token
	if token != nil {
		ctok = &cloud.Token{AccessToken: token.AccessToken}
	}
	gw, err := cloud.New(
		forge.APIURL(), url, workspace, repo, log, ctok, httpClient,
	)
	if err != nil {
		panic(err)
	}

	client, err := cloud.NewClient(
		cloud.StaticTokenSource(cloud.Token{
			AccessToken: token.AccessToken,
		}),
		&cloud.ClientOptions{
			BaseURL:    forge.APIURL(),
			HTTPClient: httpClient,
		},
	)
	if err != nil {
		panic(err)
	}

	repository, err := withReviewRepository(context.Background(), newRepository(forge, log, &testCloudGateway{
		Gateway:   gw,
		client:    client,
		token:     token,
		http:      httpClient,
		apiURL:    forge.APIURL(),
		workspace: workspace,
		repo:      repo,
	}))
	if err != nil {
		panic(err)
	}
	return repository
}
