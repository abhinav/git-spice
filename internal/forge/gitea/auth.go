package gitea

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
	// AuthTypeAPIToken indicates authentication via Gitea API token.
	AuthTypeAPIToken AuthType = iota

	// AuthTypeEnvironmentVariable indicates authentication via environment variable.
	//
	// This is not a real authentication method.
	AuthTypeEnvironmentVariable AuthType = 100
)

// AuthenticationToken defines the token returned by the Gitea forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AuthType specifies the authentication method used.
	AuthType AuthType `json:"auth_type"`

	// AccessToken is the Gitea API token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

// AuthenticationFlow prompts the user to authenticate with Gitea.
// This rejects the request if the user is already authenticated
// with a GITEA_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(
	_ context.Context,
	view ui.View,
) (forge.AuthenticationToken, error) {
	log := f.logger()

	if f.Options.Token != "" {
		log.Error("Already authenticated with GITEA_TOKEN.")
		log.Error("Unset GITEA_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	log.Info("Gitea uses API tokens for authentication.")
	log.Info("Create one from your Gitea user settings page.")

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
	tok := t.(*AuthenticationToken)

	if f.Options.Token != "" && f.Options.Token == tok.AccessToken {
		return nil
	}

	if tok.AuthType == AuthTypeEnvironmentVariable {
		return errors.New("should never save AuthTypeEnvironmentVariable")
	}

	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	f.logger().Debug("Saving authentication token to local secret storage")
	return stash.SaveSecret(f.URL(), "token", string(data))
}

// LoadAuthenticationToken loads the authentication token from the stash.
// Priority: GITEA_TOKEN env var > stored token.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	if f.Options.Token != "" {
		return &AuthenticationToken{
			AccessToken: f.Options.Token,
			AuthType:    AuthTypeEnvironmentVariable,
		}, nil
	}

	tokstr, err := stash.LoadSecret(f.URL(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	var tok AuthenticationToken
	if err := json.Unmarshal([]byte(tokstr), &tok); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &tok, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	f.logger().Debug("Clearing authentication token from local secret storage")
	return stash.DeleteSecret(f.URL(), "token")
}
