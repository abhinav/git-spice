package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
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

	// GitHubCLI is true if we should use GitHub CLI for API requests.
	//
	// If true, AccessToken is not used.
	GitHubCLI bool `json:"github_cli,omitempty"`

	// AccessToken is the GitHub access token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

func (t *AuthenticationToken) tokenSource() oauth2.TokenSource {
	if t.GitHubCLI {
		return &CLITokenSource{}
	}
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: t.AccessToken})
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

// AuthenticationFlow prompts the user to authenticate with GitHub.
// This rejects the request if the user is already authenticated
// with a GITHUB_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(ctx context.Context, view ui.View) (forge.AuthenticationToken, error) {
	log := f.logger()
	// Already authenticated with GITHUB_TOKEN.
	// If the user tries to authenticate again, we should error.
	if f.Options.Token != "" {
		// NB: alternatively, we can make this a no-op,
		// and just omit saving it to the stash.
		// Adjust based on user feedback.
		log.Error("Already authenticated with GITHUB_TOKEN.")
		log.Error("Unset GITHUB_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	oauthEndpoint, err := f.oauth2Endpoint()
	if err != nil {
		return nil, fmt.Errorf("get OAuth endpoint: %w", err)
	}

	auth, err := selectAuthenticator(view, authenticatorOptions{
		Endpoint: oauthEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("select authenticator: %w", err)
	}

	return auth.Authenticate(ctx, view)
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(stash secret.Stash, t forge.AuthenticationToken) error {
	ght := t.(*AuthenticationToken)
	if f.Options.Token != "" && f.Options.Token == ght.AccessToken {
		// If the user has set GITHUB_TOKEN,
		// we should not save it to the stash.
		return nil
	}

	bs, err := json.Marshal(ght)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	f.logger().Debug("Saving authentication token to local secret storage")
	return stash.SaveSecret(f.URL(), "token", string(bs))
}

// LoadAuthenticationToken loads the authentication token from the stash.
// If the user has set GITHUB_TOKEN, it will be used instead.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	if f.Options.Token != "" {
		// If the user has set GITHUB_TOKEN, we should use that
		// regardless of what's in the stash.
		return &AuthenticationToken{AccessToken: f.Options.Token}, nil
	}

	tokstr, err := stash.LoadSecret(f.URL(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	var tok AuthenticationToken
	if err := json.Unmarshal([]byte(tokstr), &tok); err != nil {
		// Old token format, just use it as the access token.
		return &AuthenticationToken{AccessToken: tokstr}, nil
	}

	return &tok, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	f.logger().Debug("Clearing authentication token from local secret storage")
	return stash.DeleteSecret(f.URL(), "token")
}

type authenticator interface {
	Authenticate(context.Context, ui.View) (*AuthenticationToken, error)
}

var _authenticationMethods = []struct {
	Title       string
	Description func(focused bool) string
	Build       func(authenticatorOptions) authenticator
}{
	{
		Title:       "OAuth",
		Description: oauthDesc,
		Build: func(a authenticatorOptions) authenticator {
			return &DeviceFlowAuthenticator{
				Endpoint: a.Endpoint,
				ClientID: _oauthAppClientID,
				Scopes:   []string{"repo", "read:org"},
			}
		},
	},
	{
		Title:       "OAuth: Public repositories only",
		Description: oauthPublicDesc,
		Build: func(a authenticatorOptions) authenticator {
			return &DeviceFlowAuthenticator{
				Endpoint: a.Endpoint,
				ClientID: _oauthAppClientID,
				Scopes:   []string{"public_repo"},
			}
		},
	},
	{
		Title:       "GitHub App",
		Description: githubAppDesc,
		Build: func(a authenticatorOptions) authenticator {
			return &DeviceFlowAuthenticator{
				Endpoint: a.Endpoint,
				ClientID: _githubAppClientID,
				// No scopes needed for GitHub App.
			}
		},
	},
	{
		Title:       "Personal Access Token",
		Description: patDesc,
		Build: func(authenticatorOptions) authenticator {
			return &PATAuthenticator{}
		},
	},
	{
		Title:       "GitHub CLI",
		Description: ghDesc,
		Build: func(authenticatorOptions) authenticator {
			// Offer this option only if the user
			// has the GH CLI installed.
			ghExe, err := exec.LookPath("gh")
			if err != nil {
				return nil
			}

			return &CLIAuthenticator{GH: ghExe}
		},
	},
}

// authenticatorOptions presents the user with multiple authentication methods,
// prompts them to choose one, and executes the chosen method.
type authenticatorOptions struct {
	Endpoint oauth2.Endpoint // required
}

func selectAuthenticator(view ui.View, a authenticatorOptions) (authenticator, error) {
	var methods []ui.ListItem[authenticator]
	for _, m := range _authenticationMethods {
		auth := m.Build(a)
		if auth != nil {
			methods = append(methods, ui.ListItem[authenticator]{
				Title:       m.Title,
				Description: m.Description,
				Value:       auth,
			})
		}
	}

	var method authenticator
	field := ui.NewList[authenticator]().
		WithTitle("Select an authentication method").
		WithItems(methods...).
		WithValue(&method)
	err := ui.Run(view, field)
	return method, err
}

func oauthDesc(bool) string {
	return text.Dedent(`
	Authorize git-spice to act on your behalf from this device only.
	git-spice will get access to all repositories: public and private.
	For private repositories, you will need to request installation from a repository owner.
	`)
}

func oauthPublicDesc(bool) string {
	return text.Dedent(`
	Authorize git-spice to act on your behalf from this device only.
	git-spice will only get access to public repositories.
	`)
}

func githubAppDesc(focused bool) string {
	return text.Dedentf(`
	Authorize git-spice to act on your behalf from this device only.
	git-spice will only get access to repositories where the git-spice GitHub App is installed explicitly.
	Use %[1]s to install the App on repositories.
	For private repositories, you will need to request installation from a repository owner.
	`, urlStyle(focused).Render("https://github.com/apps/git-spice"))
}

func patDesc(focused bool) string {
	scopeStyle := ui.NewStyle()
	if focused {
		scopeStyle = scopeStyle.Bold(true)
	}

	return text.Dedentf(`
	Enter a classic or fine-grained Personal Access Token generated from %[1]s.
	Classic tokens need at least one of the following scopes: %[2]s or %[3]s.
	Fine-grained tokens need read/write access to Repository %[4]s and %[5]s.
	You can use this method if you do not have the ability to install a GitHub or OAuth App on your repositories.
	`,
		urlStyle(focused).Render("https://github.com/settings/tokens"),
		scopeStyle.Render("repo"), scopeStyle.Render("public_repo"),
		scopeStyle.Render("Contents"), scopeStyle.Render("Pull requests"),
	)
}

func ghDesc(focused bool) string {
	return text.Dedentf(`
	Re-use an existing GitHub CLI (%[1]s) session.
	You must be logged into gh with 'gh auth login' for this to work.
	You can use this if you're just experimenting and don't want to set up a token yet.
	`, urlStyle(focused).Render("https://cli.github.com"))
}

func urlStyle(focused bool) lipgloss.Style {
	s := ui.NewStyle()
	if focused {
		s = s.Bold(true).Foreground(ui.Magenta).Underline(true)
	}
	return s
}

// DeviceFlowAuthenticator implements the OAuth device flow for GitHub.
// This is used for OAuth and GitHub App authentication.
type DeviceFlowAuthenticator struct {
	// Endpoint is the OAuth endpoint to use.
	Endpoint oauth2.Endpoint

	// ClientID for the OAuth or GitHub App.
	ClientID string

	// Scopes specifies the OAuth scopes to request.
	Scopes []string
}

// Authenticate executes the OAuth authentication flow.
func (a *DeviceFlowAuthenticator) Authenticate(ctx context.Context, view ui.View) (*AuthenticationToken, error) {
	cfg := oauth2.Config{
		ClientID:    a.ClientID,
		Endpoint:    a.Endpoint,
		Scopes:      a.Scopes,
		RedirectURL: "http://127.0.0.1/callback",
	}

	resp, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, err
	}

	urlStle := ui.NewStyle().Foreground(ui.Cyan).Bold(true).Underline(true)
	codeStyle := ui.NewStyle().Foreground(ui.Cyan).Bold(true)
	bullet := ui.NewStyle().PaddingLeft(2).Foreground(ui.Gray)
	faint := ui.NewStyle().Faint(true)

	fmt.Fprintf(view, "%s Visit %s\n", bullet.Render("1."), urlStle.Render(resp.VerificationURI))
	fmt.Fprintf(view, "%s Enter code: %s\n", bullet.Render("2."), codeStyle.Render(resp.UserCode))
	fmt.Fprintln(view, faint.Render("The code expires in a few minutes."))
	fmt.Fprintln(view, faint.Render("It will take a few seconds to verify after you enter it."))
	// TODO: maybe open browser with flag opt-out

	token, err := cfg.DeviceAccessToken(ctx, resp,
		oauth2.SetAuthURLParam("grant_type", "urn:ietf:params:oauth:grant-type:device_code"))
	if err != nil {
		return nil, err
	}

	return &AuthenticationToken{AccessToken: token.AccessToken}, nil
}

// PATAuthenticator implements PAT authentication for GitHub.
type PATAuthenticator struct{}

// Authenticate prompts the user for a Personal Access Token,
// validates it, and returns the token if successful.
func (a *PATAuthenticator) Authenticate(_ context.Context, view ui.View) (*AuthenticationToken, error) {
	var token string
	err := ui.Run(view,
		ui.NewInput().
			WithTitle("Enter Personal Access Token").
			WithValidate(func(input string) error {
				if strings.TrimSpace(input) == "" {
					return errors.New("token is required")
				}
				return nil
			}).WithValue(&token),
	)

	// TODO: Should we validate the token by making a request?
	return &AuthenticationToken{AccessToken: token}, err
}

// CLIAuthenticator implements GitHub CLI authentication flow.
// This doesn't do anything special besides checking if the user is logged in.
type CLIAuthenticator struct {
	GH string // required

	runCmd func(*exec.Cmd) error
}

// Authenticate checks if the user is authenticated with GitHub CLI.
func (a *CLIAuthenticator) Authenticate(context.Context, ui.View) (*AuthenticationToken, error) {
	runCmd := (*exec.Cmd).Run
	if a.runCmd != nil {
		runCmd = a.runCmd
	}

	if err := runCmd(exec.Command(a.GH, "auth", "token")); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.Join(
				errors.New("gh is not authenticated"),
				fmt.Errorf("stderr: %s", exitErr.Stderr),
			)
		}
		return nil, fmt.Errorf("run gh: %w", err)
	}

	return &AuthenticationToken{GitHubCLI: true}, nil
}
