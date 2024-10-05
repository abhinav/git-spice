package github

import (
	"net/http"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/graphqlutil"
)

func newGitHubEnterpriseClient(
	url string,
	httpClient *http.Client,
) *githubv4.Client {
	httpClient.Transport = graphqlutil.WrapTransport(httpClient.Transport)
	return githubv4.NewEnterpriseClient(url, httpClient)
}
