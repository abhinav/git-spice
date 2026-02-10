package bitbucket

import (
	"net/http"

	"go.abhg.dev/gs/internal/silog"
)

// client is an HTTP client for the Bitbucket API.
type client struct {
	baseURL string
	token   *AuthenticationToken
	http    *http.Client
	log     *silog.Logger
}

func newClient(baseURL string, token *AuthenticationToken, log *silog.Logger) *client {
	return &client{
		baseURL: baseURL,
		token:   token,
		http:    http.DefaultClient,
		log:     log,
	}
}
