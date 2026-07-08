package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket/server"
	"go.abhg.dev/gs/internal/ui"
)

// errNoServerURL reports that an operation against
// a Bitbucket Data Center instance needs an instance URL,
// but none was configured or derived from the Git remote.
var errNoServerURL = errors.New(
	"no Bitbucket Data Center URL configured: " +
		"set spice.forge.bitbucket.url or BITBUCKET_URL, " +
		"or set spice.forge.kind=bitbucket to derive it from the Git remote",
)

func (f *Forge) serverAuthenticationFlow(
	ctx context.Context,
	view ui.View,
) (forge.AuthenticationToken, error) {
	token, err := promptRequired(view,
		"Enter HTTP access token",
		f.tokenHelp(),
		"HTTP access token is required",
	)
	if err != nil {
		return nil, fmt.Errorf("prompt for HTTP access token: %w", err)
	}

	tok := &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: token,
	}

	if err := f.validateToken(ctx, tok); err != nil {
		log := f.logger()
		log.Error("Could not validate the HTTP access token.")
		log.Error("Ensure the token is valid and has Repository Write scope.")
		return nil, fmt.Errorf("validate token: %w", err)
	}

	return tok, nil
}

func (f *Forge) tokenHelp() string {
	return "Create an HTTP access token with Repository Write scope at:\n" +
		strings.TrimSuffix(f.URL(), "/") + "/plugins/servlet/access-tokens/manage"
}

func (f *Forge) validateToken(ctx context.Context, tok *AuthenticationToken) error {
	client, err := server.NewClient(
		server.StaticTokenSource(server.Token{
			AccessToken: tok.AccessToken,
		}),
		&server.ClientOptions{BaseURL: f.APIURL()},
	)
	if err != nil {
		return fmt.Errorf("create Bitbucket Data Center client: %w", err)
	}

	_, _, err = client.CurrentUser(ctx)
	return err
}
