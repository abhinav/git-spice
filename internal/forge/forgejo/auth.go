package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
)

// AuthType identifies the authentication method used.
type AuthType int

const (
	// AuthTypeAPIToken indicates authentication via Forgejo API token.
	AuthTypeAPIToken AuthType = iota

	// AuthTypeEnvironmentVariable indicates authentication via environment
	// variable.
	//
	// This is not a real authentication method.
	AuthTypeEnvironmentVariable AuthType = 100
)

// AuthenticationToken defines the token returned by the Forgejo forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AuthType specifies the authentication method used.
	AuthType AuthType `json:"auth_type"`

	// AccessToken is the Forgejo API token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

// AuthenticationFlow prompts the user to authenticate with Forgejo.
// This rejects the request if the user is already authenticated
// with a FORGEJO_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(
	_ context.Context,
	view ui.View,
) (forge.AuthenticationToken, error) {
	log := f.logger()

	if f.Options.Token != "" {
		log.Error("Already authenticated with FORGEJO_TOKEN.")
		log.Error("Unset FORGEJO_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	log.Info("Forgejo uses API tokens for authentication.")
	log.Info("Create one from your Forgejo user settings.")

	var token string
	err := ui.Run(view, ui.NewInput().
		WithTitle("Enter API token").
		WithValidate(func(input string) error {
			if strings.TrimSpace(input) == "" {
				return errors.New("API token is required")
			}
			return nil
		}).
		WithValue(&token),
	)
	if err != nil {
		return nil, fmt.Errorf("prompt for API token: %w", err)
	}

	return &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: token,
	}, nil
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(
	stash secret.Stash,
	t forge.AuthenticationToken,
) error {
	token := t.(*AuthenticationToken)
	if f.Options.Token != "" && f.Options.Token == token.AccessToken {
		return nil
	}

	if token.AuthType != AuthTypeAPIToken {
		return fmt.Errorf("unknown auth type: %d", token.AuthType)
	}
	if token.AccessToken == "" {
		return errors.New("access token is required")
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	return stash.SaveSecret(f.URL(), "token", string(data))
}

// LoadAuthenticationToken loads the authentication token from the stash.
// If the user has set FORGEJO_TOKEN, it will be used instead.
func (f *Forge) LoadAuthenticationToken(
	stash secret.Stash,
) (forge.AuthenticationToken, error) {
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

func newGatewayTokenSource(
	token *AuthenticationToken,
) (forgejo.TokenSource, error) {
	if token == nil {
		return nil, errors.New("nil authentication token")
	}

	switch token.AuthType {
	case AuthTypeAPIToken, AuthTypeEnvironmentVariable:
		return forgejo.StaticTokenSource(forgejo.Token{
			Type:  forgejo.TokenTypeAPIToken,
			Value: token.AccessToken,
		}), nil

	default:
		return nil, fmt.Errorf(
			"no source for authentication type: %v",
			token.AuthType,
		)
	}
}
