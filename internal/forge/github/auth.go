package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/dustin/go-humanize"
	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
	"golang.org/x/oauth2"
)

const (
	_oauthAppClientID  = "Ov23lin9rC3LWqd4ks2f"
	_githubAppClientID = "Iv23lifdKaAyYAHQwxNp"
	// (These are not secret.)
)

// AuthenticationToken defines the token returned by the GitHub forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	tok string
}

func (t *AuthenticationToken) githubClient(ctx context.Context, apiURL string) (*githubv4.Client, error) {
	graphQLAPIURL, err := url.JoinPath(apiURL, "/graphql")
	if err != nil {
		return nil, fmt.Errorf("build GraphQL API URL: %w", err)
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: t.tok})
	httpClient := oauth2.NewClient(ctx, tokenSource)
	return githubv4.NewEnterpriseClient(graphQLAPIURL, httpClient), nil
}

// AuthenticationFlow prompts the user to authenticate with GitHub.
// This rejects the request if the user is already authenticated
// with a GITHUB_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(ctx context.Context) (forge.AuthenticationToken, error) {
	// Already authenticated with GITHUB_TOKEN.
	// If the user tries to authenticate again, we should error.
	if f.Options.Token != "" {
		// NB: alternatively, we can make this a no-op,
		// and just omit saving it to the stash.
		// Adjust based on user feedback.
		f.Log.Error("Already authenticated with GITHUB_TOKEN.")
		f.Log.Error("Unset GITHUB_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	methods := []ui.ListItem[authenticationMethod]{
		{
			Title:       "OAuth",
			Description: _oauthDesc,
			Value: (&DeviceFlowAuthenticator{
				Forge:    f,
				Log:      f.Log,
				ClientID: _oauthAppClientID,
				Scopes:   []string{"repo"},
			}).Authenticate,
		},
		{
			Title:       "OAuth: Public repositories only",
			Description: _oauthPublicDesc,
			Value: (&DeviceFlowAuthenticator{
				Forge:    f,
				Log:      f.Log,
				ClientID: _oauthAppClientID,
				Scopes:   []string{"public_repo"},
			}).Authenticate,
		},
		{
			Title:       "GitHub App",
			Description: _githubAppDesc,
			Value: (&DeviceFlowAuthenticator{
				Forge:    f,
				Log:      f.Log,
				ClientID: _githubAppClientID,
				// No scopes needed for GitHub App.
			}).Authenticate,
		},
		{
			Title:       "Personal Access Token",
			Description: _patDesc,
			Value: (&PersonalAccessTokenAuthenticator{
				Forge:  f,
				APIURL: f.Options.APIURL,
				Log:    f.Log,
			}).Authenticate,
		},
	}

	var method authenticationMethod
	field := ui.NewList[authenticationMethod]().
		WithTitle("Select an authentication method").
		WithItems(methods...).
		WithValue(&method)
	if err := ui.Run(field); err != nil {
		return nil, err
	}

	return method(ctx)
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(stash secret.Stash, t forge.AuthenticationToken) error {
	tok := t.(*AuthenticationToken).tok
	if f.Options.Token != "" && f.Options.Token == tok {
		// If the user has set GITHUB_TOKEN,
		// we should not save it to the stash.
		return nil
	}
	return stash.SaveSecret(f.URL(), "token", tok)
}

// LoadAuthenticationToken loads the authentication token from the stash.
// If the user has set GITHUB_TOKEN, it will be used instead.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	if f.Options.Token != "" {
		// If the user has set GITHUB_TOKEN, we should use that
		// regardless of what's in the stash.
		return &AuthenticationToken{tok: f.Options.Token}, nil
	}

	tok, err := stash.LoadSecret(f.URL(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	return &AuthenticationToken{tok: tok}, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	return stash.DeleteSecret(f.URL(), "token")
}

type authenticationMethod func(context.Context) (forge.AuthenticationToken, error)

var _oauthDesc = strings.TrimSpace(`
Authorize git-spice to act on your behalf from this device only.
git-spice will get access to all repositories: public and private.
`)

var _oauthPublicDesc = strings.TrimSpace(`
Authorize git-spice to act on your behalf from this device only.
git-spice will only get access to public repositories.
`)

var _githubAppDesc = strings.TrimSpace(`
Authorize git-spice to act on your behalf from this device only.
git-spice will only get access to repositories where the git-spice GitHub App is installed explicitly.
Use https://github.com/apps/git-spice to install the App on repositories.
`)

var _patDesc = strings.TrimSpace(`
Enter a classic or fine-grained Personal Access Token generated from https://github.com/settings/tokens.
Classic tokens need at least one of the following scopes: repo or public_repo.
Fine-grained tokens need read/write access to Repository Contents and Pull requests.
You can use this method if you do not have the ability to install a GitHub or OAuth App on your repositories.
`)

// DeviceFlowAuthenticator implements the OAuth device flow for GitHub.
// This is used for OAuth and GitHub App authentication.
type DeviceFlowAuthenticator struct {
	// Forge is the forge to authenticate with.
	Forge *Forge

	// ClientID for the OAuth or GitHub App.
	ClientID string

	// Scopes specifies the OAuth scopes to request.
	Scopes []string

	// Log is the logger to use for logging.
	Log *log.Logger
}

// Authenticate executes the OAuth authentication flow.
func (a *DeviceFlowAuthenticator) Authenticate(ctx context.Context) (forge.AuthenticationToken, error) {
	log := a.Log
	endpoint, err := a.Forge.oauth2Endpoint()
	if err != nil {
		return nil, err
	}

	cfg := oauth2.Config{
		ClientID:    a.ClientID,
		Endpoint:    endpoint,
		Scopes:      a.Scopes,
		RedirectURL: "http://127.0.0.1/callback",
	}

	resp, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, err
	}

	log.Infof("Please visit %s and enter the following code", resp.VerificationURI)
	log.Infof("    %s", resp.UserCode) // TODO: styling
	log.Infof("The code expires %s", humanize.Time(resp.Expiry))
	log.Infof("It will take %v or more seconds to verify the code after you enter it", resp.Interval)
	// TODO: open browser with flag opt-out

	token, err := cfg.DeviceAccessToken(ctx, resp,
		oauth2.SetAuthURLParam("grant_type", "urn:ietf:params:oauth:grant-type:device_code"))
	if err != nil {
		return nil, err
	}

	return &AuthenticationToken{tok: token.AccessToken}, nil
}

func (f *Forge) oauth2Endpoint() (oauth2.Endpoint, error) {
	u, err := url.Parse(f.URL())
	if err != nil {
		return oauth2.Endpoint{}, fmt.Errorf("bad GitHub URL: %w", err)
	}

	return oauth2.Endpoint{
		AuthURL:       u.JoinPath("/login/oauth/authorize").String(),
		TokenURL:      u.JoinPath("/login/oauth/access_token").String(),
		DeviceAuthURL: u.JoinPath("/login/device/code").String(),
	}, nil
}

// PersonalAccessTokenAuthenticator implements PAT authentication for GitHub.
type PersonalAccessTokenAuthenticator struct {
	// Forge is the forge to authenticate with.
	Forge *Forge

	// APIURL is the URL at which the GitHub API is hosted.
	APIURL string

	// Log is used for logging messages to the user.
	Log *log.Logger
}

// Authenticate prompts the user for a Personal Access Token,
// validates it, and returns the token if successful.
func (a *PersonalAccessTokenAuthenticator) Authenticate(ctx context.Context) (forge.AuthenticationToken, error) {
	var token string
	err := ui.Run(ui.NewInput().
		WithTitle("Enter Personal Access Token").
		WithValidate(func(input string) error {
			if strings.TrimSpace(input) == "" {
				return errors.New("token is required")
			}
			return nil
		}).
		WithValue(&token))
	if err != nil {
		return nil, err
	}

	// TODO: validate token by making a request to the API

	return &AuthenticationToken{tok: token}, nil
}