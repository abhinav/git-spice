package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"golang.org/x/oauth2"
)

// AuthenticationToken defines the token returned by the GitLab forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AccessToken is the GitLab access token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

func (f *Forge) oauth2Endpoint() (oauth2.Endpoint, error) {
	u, err := url.Parse(f.URL())
	if err != nil {
		return oauth2.Endpoint{}, fmt.Errorf("bad GitLab URL: %w", err)
	}

	return oauth2.Endpoint{
		AuthURL:       u.JoinPath("/oauth/authorize").String(),
		TokenURL:      u.JoinPath("/oauth/access_token").String(),
		DeviceAuthURL: u.JoinPath("/oauth/authorize_device").String(),
	}, nil
}

// AuthenticationFlow prompts the user to authenticate with GitLab.
// This rejects the request if the user is already authenticated
// with a GITLAB_TOKEN environment variable.
func (f *Forge) AuthenticationFlow(ctx context.Context) (forge.AuthenticationToken, error) {
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

	auth, err := selectAuthenticator(authenticatorOptions{
		Endpoint: oauthEndpoint,
		Stdin:    os.Stdin,
		Stderr:   os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("select authenticator: %w", err)
	}

	return auth.Authenticate(ctx)
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
		return &AuthenticationToken{AccessToken: f.Options.Token}, nil
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
	return stash.DeleteSecret(f.URL(), "token")
}

type authenticator interface {
	Authenticate(context.Context) (*AuthenticationToken, error)
}

var _authenticationMethods = []struct {
	Title       string
	Description func(focused bool) string
	Build       func(authenticatorOptions) authenticator
}{
	// TODO: OAuth
	{
		Title:       "Personal Access Token",
		Description: patDesc,
		Build: func(a authenticatorOptions) authenticator {
			return &PATAuthenticator{
				Stdin:  a.Stdin,
				Stderr: a.Stderr,
			}
		},
	},
	// TODO: GitLab CLI
}

// authenticatorOptions presents the user with multiple authentication methods,
// prompts them to choose one, and executes the chosen method.
type authenticatorOptions struct {
	Endpoint oauth2.Endpoint // required
	Stdin    io.Reader       // required
	Stderr   io.Writer       // required
}

func selectAuthenticator(a authenticatorOptions) (authenticator, error) {
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
	err := ui.Run(field, ui.WithInput(a.Stdin), ui.WithOutput(a.Stderr))
	return method, err
}

func patDesc(focused bool) string {
	scopeStyle := ui.NewStyle()
	if focused {
		scopeStyle = scopeStyle.Bold(true)
	}

	return text.Dedentf(`
	Enter a classic or fine-grained Personal Access Token generated from %[1]s.
	The Personal Access Token need the following scope: %[2]s.
	`,
		urlStyle(focused).Render("https://gitlab.com/-/user_settings/personal_access_tokens"),
		scopeStyle.Render("api"),
	)
}

func urlStyle(focused bool) lipgloss.Style {
	s := ui.NewStyle()
	if focused {
		s = s.Bold(true).Foreground(ui.Magenta).Underline(true)
	}
	return s
}

// PATAuthenticator implements PAT authentication for GitLab.
type PATAuthenticator struct {
	Stdin  io.Reader // required
	Stderr io.Writer // required
}

// Authenticate prompts the user for a Personal Access Token,
// validates it, and returns the token if successful.
func (a *PATAuthenticator) Authenticate(_ context.Context) (*AuthenticationToken, error) {
	var token string
	err := ui.Run(ui.NewInput().
		WithTitle("Enter Personal Access Token").
		WithValidate(func(input string) error {
			if strings.TrimSpace(input) == "" {
				return errors.New("token is required")
			}
			return nil
		}).WithValue(&token),
		ui.WithInput(a.Stdin),
		ui.WithOutput(a.Stderr),
	)

	// TODO: Should we validate the token by making a request?
	return &AuthenticationToken{AccessToken: token}, err
}
