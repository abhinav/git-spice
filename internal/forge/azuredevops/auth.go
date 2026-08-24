package azuredevops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/xec"
)

// AuthenticationToken defines the token returned by the Azure DevOps forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	// AuthType specifies the kind of authentication method used.
	AuthType AuthType `json:"auth_type,omitempty"`

	// AccessToken is the Azure DevOps access token.
	AccessToken string `json:"access_token,omitempty"`
}

var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)

// AuthType specifies the kind of authentication method used.
type AuthType int

const (
	// AuthTypePAT states that PAT authentication was used.
	AuthTypePAT AuthType = iota

	// AuthTypeAzureCLI states that Azure CLI authentication was used.
	AuthTypeAzureCLI

	// AuthTypeEnvironmentVariable states
	// that the token was set via an environment variable.
	AuthTypeEnvironmentVariable AuthType = 100
)

// MarshalText implements encoding.TextMarshaler.
func (a AuthType) MarshalText() ([]byte, error) {
	switch a {
	case AuthTypePAT:
		return []byte("pat"), nil
	case AuthTypeAzureCLI:
		return []byte("azure-cli"), nil
	case AuthTypeEnvironmentVariable:
		return nil, errors.New("should never save AuthTypeEnvironmentVariable")
	default:
		return nil, fmt.Errorf("unknown auth type: %d", a)
	}
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (a *AuthType) UnmarshalText(b []byte) error {
	switch string(b) {
	case "pat":
		*a = AuthTypePAT
	case "azure-cli":
		*a = AuthTypeAzureCLI
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
	case AuthTypeAzureCLI:
		return "Azure CLI"
	case AuthTypeEnvironmentVariable:
		return "Environment Variable"
	default:
		return fmt.Sprintf("AuthType(%d)", int(a))
	}
}

// AuthenticationFlow prompts the user to authenticate with Azure DevOps.
// This rejects the request if the user is already authenticated
// with an AZURE_DEVOPS_PAT environment variable.
func (f *Forge) AuthenticationFlow(
	ctx context.Context,
	view ui.View,
) (forge.AuthenticationToken, error) {
	log := f.logger()

	// Already authenticated with AZURE_DEVOPS_PAT.
	if f.Options.Token != "" {
		log.Error("Already authenticated with AZURE_DEVOPS_PAT.")
		log.Error("Unset AZURE_DEVOPS_PAT to login with a different method.")
		return nil, errors.New("already authenticated")
	}

	auth, err := selectAuthenticator(view, authenticatorOptions{})
	if err != nil {
		return nil, fmt.Errorf("select authenticator: %w", err)
	}

	return auth.Authenticate(ctx, view)
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(
	stash secret.Stash,
	t forge.AuthenticationToken,
) error {
	adt := t.(*AuthenticationToken)
	if f.Options.Token != "" && f.Options.Token == adt.AccessToken {
		// If the user has set AZURE_DEVOPS_PAT,
		// we should not save it to the stash.
		return nil
	}

	// Validate before saving.
	switch adt.AuthType {
	case AuthTypePAT, AuthTypeAzureCLI:
		if adt.AccessToken == "" {
			return errors.New("access token is required")
		}

	case AuthTypeEnvironmentVariable:
		return errors.New("should never save AuthTypeEnvironmentVariable")

	default:
		return fmt.Errorf("unknown auth type: %d", adt.AuthType)
	}

	bs, err := json.Marshal(adt)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	f.logger().Debug("Saving authentication token to local secret storage")
	return stash.SaveSecret(f.URL(), "token", string(bs))
}

// LoadAuthenticationToken loads the authentication token from the stash.
// If the user has set AZURE_DEVOPS_PAT, it will be used instead.
func (f *Forge) LoadAuthenticationToken(
	stash secret.Stash,
) (forge.AuthenticationToken, error) {
	if f.Options.Token != "" {
		// If the user has set AZURE_DEVOPS_PAT, we should use that
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

var _execLookPath = xec.LookPath

var _authenticationMethods = []struct {
	Title       string
	Description func(ui.Theme, bool) string
	Build       func(authenticatorOptions) authenticator
}{
	{
		Title:       "Azure CLI",
		Description: azDesc,
		Build: func(a authenticatorOptions) authenticator {
			// Offer this option only if the user has Azure CLI installed.
			azExe, err := _execLookPath("az")
			if err != nil {
				return nil
			}

			return &CLIAuthenticator{
				AZ: azExe,
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
}

type authenticatorOptions struct {
	// Reserved for future options.
}

func selectAuthenticator(
	view ui.View,
	a authenticatorOptions,
) (authenticator, error) {
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

func patDesc(theme ui.Theme, focused bool) string {
	scopeStyle := ui.NewStyle()
	if focused {
		scopeStyle = scopeStyle.Bold(true)
	}

	return text.Dedentf(`
	Enter a Personal Access Token generated from %[1]s.
	The token needs at least one of the following scopes: %[2]s.
	`,
		urlStyle(focused).Render(
			theme,
			"https://dev.azure.com/{organization}/_usersSettings/tokens",
		),
		scopeStyle.Render(theme, "Code (Read & Write)"),
	)
}

func azDesc(theme ui.Theme, focused bool) string {
	return text.Dedentf(`
	Re-use an existing Azure CLI (%[1]s) session.
	You must be logged in with 'az login' for this to work.
	This is the recommended method
	if you already have the Azure CLI installed.
	`, urlStyle(focused).Render(theme, "https://learn.microsoft.com/cli/azure"))
}

func urlStyle(focused bool) ui.Style {
	s := ui.NewStyle()
	if focused {
		s = s.Bold(true).Foreground(ui.Magenta).Underline(true)
	}
	return s
}

// PATAuthenticator implements PAT authentication for Azure DevOps.
type PATAuthenticator struct{}

// Authenticate prompts the user for a Personal Access Token,
// validates it, and returns the token if successful.
func (a *PATAuthenticator) Authenticate(
	_ context.Context,
	view ui.View,
) (*AuthenticationToken, error) {
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

	return &AuthenticationToken{
		AccessToken: token,
		AuthType:    AuthTypePAT,
	}, err
}

// CLIAuthenticator implements Azure CLI authentication flow.
type CLIAuthenticator struct {
	AZ string // path to the az executable

	execer xec.Execer
}

// _azureDevOpsResourceID is the resource ID for Azure DevOps
// used when fetching OAuth tokens from Azure CLI.
const _azureDevOpsResourceID = "499b84ac-1321-427f-aa17-267ca6975798"

// Authenticate fetches an access token from Azure CLI.
func (a *CLIAuthenticator) Authenticate(
	ctx context.Context,
	_ ui.View,
) (*AuthenticationToken, error) {
	// First check if the user is logged in.
	cmd := xec.Command(ctx, nil, a.AZ, "account", "show").WithExecer(a.execer)
	if err := cmd.Run(); err != nil {
		var exitErr *xec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.Join(
				errors.New("az is not authenticated"),
				fmt.Errorf("stderr: %s", exitErr.Stderr),
			)
		}
		return nil, fmt.Errorf("run az account show: %w", err)
	}

	token, err := getAzureCLIToken(ctx, a.AZ, a.execer)
	if err != nil {
		return nil, err
	}

	return &AuthenticationToken{
		AccessToken: token,
		AuthType:    AuthTypeAzureCLI,
	}, nil
}

// getAzureCLIToken fetches a fresh Azure DevOps access token
// from the Azure CLI.
//
// az is the path to the 'az' executable.
// execer is the command executor, or nil to use the default.
func getAzureCLIToken(
	ctx context.Context,
	az string,
	execer xec.Execer,
) (string, error) {
	var stdout bytes.Buffer
	tokenCmd := xec.Command(ctx, nil, az,
		"account", "get-access-token",
		"--resource", _azureDevOpsResourceID,
		"--query", "accessToken",
		"--output", "tsv",
	).WithExecer(execer).WithStdout(&stdout)

	if err := tokenCmd.Run(); err != nil {
		var exitErr *xec.ExitError
		if errors.As(err, &exitErr) {
			return "", errors.Join(
				errors.New("failed to get Azure DevOps access token"),
				fmt.Errorf("stderr: %s", exitErr.Stderr),
			)
		}
		return "", fmt.Errorf("get access token: %w", err)
	}

	token := strings.TrimSpace(stdout.String())
	if token == "" {
		return "", errors.New("no access token returned from Azure CLI")
	}

	return token, nil
}
