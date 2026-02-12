package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
)

// AuthType identifies the authentication method used.
type AuthType int

const (
	// AuthTypeAPIToken indicates authentication via API token.
	AuthTypeAPIToken AuthType = iota

	// AuthTypeEnvironmentVariable indicates authentication via environment variable.
	// This is set to 100 to distinguish from user-selected auth types.
	AuthTypeEnvironmentVariable AuthType = 100
)

// AuthenticationToken defines the token returned by the Bitbucket forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AuthType specifies the authentication method used.
	AuthType AuthType `json:"auth_type"`

	// AccessToken is the Bitbucket API token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

// AuthenticationFlow prompts the user to authenticate with Bitbucket.
// This rejects the request if the user is already authenticated
// with a BITBUCKET_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(
	ctx context.Context,
	view ui.View,
) (forge.AuthenticationToken, error) {
	log := f.logger()

	if f.Options.Token != "" {
		log.Error("Already authenticated with BITBUCKET_TOKEN.")
		log.Error("Unset BITBUCKET_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	// For now, only API token auth is supported.
	// GCM integration can be added in a future PR.
	return f.apiTokenAuth(ctx, view)
}

func (f *Forge) apiTokenAuth(_ context.Context, view ui.View) (*AuthenticationToken, error) {
	f.logger().Info("Bitbucket Cloud uses API tokens for authentication.")
	f.logger().Info("Create one at: https://bitbucket.org/account/settings/api-tokens/")
	f.logger().Info("Required scopes: pullrequest:write, account")
	f.logger().Info("  pullrequest:write - create and edit pull requests")
	f.logger().Info("  account - read workspace members for reviewer lookup")

	token, err := promptRequired(view, "Enter API token", "API token is required")
	if err != nil {
		return nil, fmt.Errorf("prompt for API token: %w", err)
	}

	return &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: token,
	}, nil
}

func promptRequired(view ui.View, title, errMsg string) (string, error) {
	var value string
	err := ui.Run(view, ui.NewInput().
		WithTitle(title).
		WithValidate(requiredValidator(errMsg)).
		WithValue(&value),
	)
	return value, err
}

func requiredValidator(errMsg string) func(string) error {
	return func(input string) error {
		if strings.TrimSpace(input) == "" {
			return errors.New(errMsg)
		}
		return nil
	}
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(
	stash secret.Stash,
	t forge.AuthenticationToken,
) error {
	bbt := t.(*AuthenticationToken)

	// If the user has set BITBUCKET_TOKEN, we should not save it to the stash.
	if f.Options.Token != "" && f.Options.Token == bbt.AccessToken {
		return nil
	}

	data, err := json.Marshal(bbt)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	return stash.SaveSecret(f.URL(), "token", string(data))
}

// LoadAuthenticationToken loads the authentication token from the stash.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	// Environment variable takes precedence.
	if f.Options.Token != "" {
		return &AuthenticationToken{
			AuthType:    AuthTypeEnvironmentVariable,
			AccessToken: f.Options.Token,
		}, nil
	}

	data, err := stash.LoadSecret(f.URL(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	var token AuthenticationToken
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &token, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	return stash.DeleteSecret(f.URL(), "token")
}
