package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
)

// AuthType identifies the authentication method used.
type AuthType int

const (
	// AuthTypeAPIToken indicates authentication via API token.
	AuthTypeAPIToken AuthType = iota

	// AuthTypeGCM indicates authentication via git-credential-manager.
	// GCM stores OAuth tokens obtained through browser-based authentication.
	AuthTypeGCM

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

// authMethod identifies a user-selectable authentication method.
type authMethod int

const (
	authMethodGCM authMethod = iota
	authMethodAPIToken
)

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

	method, err := f.selectAuthMethod(view)
	if err != nil {
		return nil, fmt.Errorf("select auth method: %w", err)
	}

	switch method {
	case authMethodGCM:
		return f.gcmAuth(ctx, log)
	case authMethodAPIToken:
		return f.apiTokenAuth(ctx, view)
	default:
		return nil, fmt.Errorf("unknown auth method: %d", method)
	}
}

func (f *Forge) selectAuthMethod(view ui.View) (authMethod, error) {
	methods := []ui.ListItem[authMethod]{
		{
			Title:       "Git Credential Manager",
			Description: gcmAuthDescription,
			Value:       authMethodGCM,
		},
		{
			Title:       "API Token",
			Description: apiTokenAuthDescription,
			Value:       authMethodAPIToken,
		},
	}

	var method authMethod
	err := ui.Run(view,
		ui.NewList[authMethod]().
			WithTitle("Select an authentication method").
			WithItems(methods...).
			WithValue(&method),
	)
	return method, err
}

func gcmAuthDescription(bool) string {
	return "Use OAuth credentials from git-credential-manager.\n" +
		"You must have GCM installed and already authenticated."
}

func apiTokenAuthDescription(bool) string {
	return "Enter an API token manually.\n" +
		"Create one at https://bitbucket.org/account/settings/api-tokens/"
}

func (f *Forge) gcmAuth(ctx context.Context, log *silog.Logger) (*AuthenticationToken, error) {
	token, err := f.loadGCMCredentials(ctx)
	if err != nil {
		log.Error("Could not load credentials from git-credential-manager.")
		log.Error("Ensure GCM is installed and you have authenticated to Bitbucket.")
		return nil, fmt.Errorf("load GCM credentials: %w", err)
	}

	log.Info("Successfully loaded credentials from git-credential-manager.")
	return token, nil
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
// Priority order:
//  1. Environment variable (BITBUCKET_TOKEN)
//  2. Stored token in secret stash
//  3. git-credential-manager (GCM)
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	// Environment variable takes highest precedence.
	if f.Options.Token != "" {
		return &AuthenticationToken{
			AuthType:    AuthTypeEnvironmentVariable,
			AccessToken: f.Options.Token,
		}, nil
	}

	// Try stored token next.
	if token, err := f.loadStoredToken(stash); err == nil {
		return token, nil
	}

	// Fall back to git-credential-manager.
	// No caller-provided context here; use Background.
	if token, err := f.loadGCMCredentials(context.Background()); err == nil {
		f.logger().Debug("Using credentials from git-credential-manager")
		return token, nil
	}

	return nil, errors.New("no authentication token available")
}

func (f *Forge) loadStoredToken(stash secret.Stash) (*AuthenticationToken, error) {
	data, err := stash.LoadSecret(f.URL(), "token")
	if err != nil {
		return nil, err
	}

	var token AuthenticationToken
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	return stash.DeleteSecret(f.URL(), "token")
}

// loadGCMCredentials attempts to load OAuth credentials
// from git-credential-manager.
// Returns an error if GCM credentials are not available.
func (f *Forge) loadGCMCredentials(ctx context.Context) (*AuthenticationToken, error) {
	cred, err := forge.LoadGCMCredential(ctx, f.URL())
	if err != nil {
		return nil, err
	}

	return &AuthenticationToken{
		AuthType:    AuthTypeGCM,
		AccessToken: cred.Password,
	}, nil
}
