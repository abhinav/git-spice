package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"golang.org/x/oauth2"
)

// (This is not secret.)
const _oauthAppID = "3467e093f73e133c18ea6008817c00f2c91ac2ee0ec60d6be8aca6fa7c64f7c1"

// AuthenticationToken defines the token returned by the GitLab forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AuthType specifies the kind of authentication method used.
	//
	// If AuthTypeGitLabCLI, AccessToken is not used.
	AuthType AuthType `json:"auth_type,omitempty"` // required

	// AccessToken is the GitLab access token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

// AuthType specifies the kind of authentication method used.
type AuthType int

const (
	// AuthTypePAT states that PAT authentication was used.
	AuthTypePAT AuthType = iota

	// AuthTypeOAuth2 states that OAuth2 authentication was used.
	AuthTypeOAuth2

	// AuthTypeGitLabCLI states that GitLab CLI authentication was used.
	AuthTypeGitLabCLI

	// AuthTypeEnvironmentVariable states
	// that the token was set via an environment variable.
	//
	// This is not a real authentication method.
	AuthTypeEnvironmentVariable AuthType = 100
)

// MarshalText implements encoding.TextMarshaler.
func (a AuthType) MarshalText() ([]byte, error) {
	switch a {
	case AuthTypePAT:
		return []byte("pat"), nil
	case AuthTypeOAuth2:
		return []byte("oauth2"), nil
	case AuthTypeGitLabCLI:
		return []byte("gitlab-cli"), nil
	case AuthTypeEnvironmentVariable:
		return nil, fmt.Errorf("should never save AuthTypeEnvironmentVariable")
	default:
		return nil, fmt.Errorf("unknown auth type: %d", a)
	}
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (a *AuthType) UnmarshalText(b []byte) error {
	switch string(b) {
	case "pat":
		*a = AuthTypePAT
	case "oauth2":
		*a = AuthTypeOAuth2
	case "gitlab-cli":
		*a = AuthTypeGitLabCLI
	default:
		return fmt.Errorf("unknown auth type: %q", b)
	}
	return nil
}

// String returns the string representation of the AuthType.
func (a AuthType) String() string {
	switch a {
	case AuthTypePAT:
		return "Personal Access Token"
	case AuthTypeOAuth2:
		return "OAuth2"
	case AuthTypeGitLabCLI:
		return "GitLab CLI"
	case AuthTypeEnvironmentVariable:
		return "Environment Variable"
	default:
		return fmt.Sprintf("AuthType(%d)", int(a))
	}
}

func (f *Forge) oauth2Endpoint() (oauth2.Endpoint, error) {
	u, err := url.Parse(f.URL())
	if err != nil {
		return oauth2.Endpoint{}, fmt.Errorf("bad GitLab URL: %w", err)
	}

	return oauth2.Endpoint{
		AuthURL:       u.JoinPath("/oauth/authorize").String(),
		TokenURL:      u.JoinPath("/oauth/token").String(),
		DeviceAuthURL: u.JoinPath("/oauth/authorize_device").String(),
	}, nil
}

// AuthenticationFlow prompts the user to authenticate with GitLab.
// This rejects the request if the user is already authenticated
// with a GITLAB_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(ctx context.Context, view ui.View) (forge.AuthenticationToken, error) {
	// Already authenticated with GITLAB_TOKEN.
	// If the user tries to authenticate again, we should error.
	if f.Options.Token != "" {
		// NB: alternatively, we can make this a no-op,
		// and just omit saving it to the stash.
		// Adjust based on user feedback.
		f.Log.Error("Already authenticated with GITLAB_TOKEN.")
		f.Log.Error("Unset GITLAB_TOKEN to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	oauthEndpoint, err := f.oauth2Endpoint()
	if err != nil {
		return nil, fmt.Errorf("get OAuth endpoint: %w", err)
	}

	auth, err := selectAuthenticator(view, authenticatorOptions{
		Endpoint: oauthEndpoint,
		ClientID: f.Options.ClientID,
		BaseURL:  f.URL(),
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
		// If the user has set GITLAB_TOKEN,
		// we should not save it to the stash.
		return nil
	}

	bs, err := json.Marshal(ght)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	return stash.SaveSecret(f.URL(), "token", string(bs))
}

// LoadAuthenticationToken loads the authentication token from the stash.
// If the user has set GITLAB_TOKEN, it will be used instead.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	if f.Options.Token != "" {
		// If the user has set GITLAB_TOKEN, we should use that
		// regardless of what's in the stash.
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

	if tok.AuthType == AuthTypeGitLabCLI {
		return getTokenFromGitlabCLI(f.URL())
	}

	return &tok, nil
}

func getTokenFromGitlabCLI(baseURL string) (forge.AuthenticationToken, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	hostname := u.Hostname()

	cli, err := exec.Command("glab",
		"auth", "status", "-h", hostname, "-t").CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.Join(
				errors.New("glab is not authenticated"),
				fmt.Errorf("stderr: %s", exitErr.Stderr),
			)
		}
		return nil, fmt.Errorf("run gl: %w", err)
	}

	re := regexp.MustCompile(`.*Token: ([a-f0-9]+).*`)
	matches := re.FindStringSubmatch(string(cli))
	if len(matches) < 2 {
		return nil, errors.New("token not found in glab output")
	}
	token := matches[1]

	return &AuthenticationToken{
		AuthType:    AuthTypeGitLabCLI,
		AccessToken: token,
	}, nil
}

func (a *authenticatorOptions) oauth2ClientID() string {
	if a.ClientID != "" {
		return a.ClientID
	}
	return _oauthAppID
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
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
				ClientID: a.oauth2ClientID(),
				Endpoint: a.Endpoint,
				Scopes:   []string{"api"},
			}
		},
	},
	{
		Title:       "Personal Access Token",
		Description: patDesc,
		Build: func(a authenticatorOptions) authenticator {
			return &PATAuthenticator{}
		},
	},
	{
		Title:       "GitLab CLI",
		Description: glDesc,
		Build: func(a authenticatorOptions) authenticator {
			// Offer this option only if the user
			// has the GitLab CLI installed.
			glExe, err := exec.LookPath("glab")
			if err != nil {
				return nil
			}

			return &CLIAuthenticator{
				GL:      glExe,
				BaseURL: a.BaseURL,
			}
		},
	},
}

// authenticatorOptions presents the user with multiple authentication methods,
// prompts them to choose one, and executes the chosen method.
type authenticatorOptions struct {
	Endpoint oauth2.Endpoint // required

	ClientID string // required

	BaseURL string // required
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

func oauthDesc(focused bool) string {
	return text.Dedent(`
	Authorize git-spice to act on your behalf from this device only.
	git-spice will get access to all repositories: public and private.
	For private repositories, you will need to request installation from a repository owner.
	`)
}

func patDesc(focused bool) string {
	scopeStyle := ui.NewStyle()
	if focused {
		scopeStyle = scopeStyle.Bold(true)
	}

	return text.Dedentf(`
	Enter a Personal Access Token generated from %[1]s.
	The Personal Access Token need the following scope: %[2]s.
	`,
		urlStyle(focused).Render("https://gitlab.com/-/user_settings/personal_access_tokens"),
		scopeStyle.Render("api"),
	)
}

func glDesc(focused bool) string {
	return text.Dedentf(`
	Re-use an existing GitLab CLI (%[1]s) session.
	You must be logged into glab with 'glab auth login' for this to work.
	You can use this if you're just experimenting and don't want to set up a token yet.
	`, urlStyle(focused).Render("https://gitlab.com/gitlab-org/cli"))
}

func urlStyle(focused bool) lipgloss.Style {
	s := ui.NewStyle()
	if focused {
		s = s.Bold(true).Foreground(ui.Magenta).Underline(true)
	}
	return s
}

// TODO: share authenticators with GitHub

// PATAuthenticator implements PAT authentication for GitLab.
type PATAuthenticator struct{}

// Authenticate prompts the user for a Personal Access Token,
// validates it, and returns the token if successful.
func (a *PATAuthenticator) Authenticate(ctx context.Context, view ui.View) (*AuthenticationToken, error) {
	var token string
	err := ui.Run(view, ui.NewInput().
		WithTitle("Enter Personal Access Token").
		WithValidate(func(input string) error {
			if strings.TrimSpace(input) == "" {
				return errors.New("token is required")
			}
			return nil
		}).WithValue(&token),
	)

	// TODO: Should we validate the token by making a request?
	return &AuthenticationToken{
		AccessToken: token,
		AuthType:    AuthTypePAT,
	}, err
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

	return &AuthenticationToken{
		AccessToken: token.AccessToken,
		AuthType:    AuthTypeOAuth2,
	}, nil
}

// CLIAuthenticator implements GitLab CLI authentication flow.
// This doesn't do anything special besides checking if the user is logged in.
type CLIAuthenticator struct {
	GL string // required

	BaseURL string // required

	runCmd func(*exec.Cmd) error // for testing
}

// Authenticate checks if the user is authenticated with GitHub CLI.
func (a *CLIAuthenticator) Authenticate(ctx context.Context, _ ui.View) (*AuthenticationToken, error) {
	runCmd := (*exec.Cmd).Run
	if a.runCmd != nil {
		runCmd = a.runCmd
	}

	u, err := url.Parse(a.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	baseURL := u.Hostname()

	if err := runCmd(exec.CommandContext(ctx, a.GL,
		"auth", "status", "-h", baseURL, "-t")); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.Join(
				errors.New("glab is not authenticated"),
				fmt.Errorf("stderr: %s", exitErr.Stderr),
			)
		}
		return nil, fmt.Errorf("run gl: %w", err)
	}

	return &AuthenticationToken{
		AuthType: AuthTypeGitLabCLI,
	}, nil
}
