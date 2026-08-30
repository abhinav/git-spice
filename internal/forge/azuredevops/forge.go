// Package azuredevops provides a wrapper around Azure DevOps APIs
// in a manner compliant with the [forge.Forge] interface.
package azuredevops

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// DefaultURL is the default base URL for Azure DevOps Services.
const DefaultURL = "https://dev.azure.com"

// Options defines command line options for the Azure DevOps Forge.
// These are all hidden in the CLI,
// and are expected to be set only via environment variables.
type Options struct {
	// URL is the URL for Azure DevOps.
	// Override this for testing or Azure DevOps Server.
	URL string `name:"azuredevops-url" hidden:"" config:"forge.azuredevops.url" env:"AZURE_DEVOPS_URL" help:"Base URL for Azure DevOps web requests"`

	// Token is a fixed token used to authenticate with Azure DevOps.
	// This may be used to skip the login flow.
	Token string `name:"azuredevops-token" hidden:"" env:"AZURE_DEVOPS_PAT" help:"Azure DevOps Personal Access Token"`
}

// Definition configures Azure DevOps forge instances.
type Definition struct {
	changeMetadataCodec

	// Options stores CLI and environment configuration.
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var (
	_ forge.Definition = (*Definition)(nil)
	_ forge.Forge      = (*Forge)(nil)
)

// ID reports a unique key for this forge.
func (*Definition) ID() string { return "azuredevops" }

// BaseURL reports the Azure DevOps web URL used for host matching.
func (d *Definition) BaseURL() string {
	return cmp.Or(d.Options.URL, DefaultURL)
}

// MatchesRemoteURL reports whether the remote belongs to Azure DevOps.
func (d *Definition) MatchesRemoteURL(remoteURL *giturl.URL) bool {
	if d.Options.URL != "" {
		return forge.ValidateRemoteURL(d.Options.URL, remoteURL) == nil
	}
	if remoteURL == nil {
		return false
	}

	host := strings.ToLower(remoteURL.Hostname)
	return host == "dev.azure.com" ||
		strings.HasSuffix(host, ".dev.azure.com") ||
		legacyOrganization(remoteURL) != ""
}

// CLIPlugin returns the CLI plugin for the Azure DevOps Forge.
func (d *Definition) CLIPlugin() any { return &d.Options }

// New constructs an Azure DevOps Forge from the configured options.
func (d *Definition) New(remoteURL *giturl.URL) (forge.Forge, error) {
	if err := forge.ValidateRemoteURL(d.Options.URL, remoteURL); err != nil {
		return nil, err
	}

	return &Forge{
		Options:            d.Options,
		baseURL:            d.BaseURL(),
		legacyOrganization: legacyOrganization(remoteURL),
		Log:                d.Log,
	}, nil
}

// Forge builds an Azure DevOps Forge.
type Forge struct {
	changeMetadataCodec

	Options            Options
	baseURL            string
	legacyOrganization string

	// Log specifies the logger to use.
	Log *silog.Logger

	// Execer is the command executor.
	// If nil, the default executor is used.
	Execer xec.Execer
}

func (f *Forge) logger() *silog.Logger {
	if f.Log == nil {
		return silog.Nop()
	}
	return f.Log.WithPrefix("azuredevops")
}

// URL returns the base URL configured for the Azure DevOps Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// BaseURL reports the Azure DevOps web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.baseURL
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "azuredevops" }

// ParseRepositoryPath parses an Azure DevOps repository path
// and returns a [RepositoryID] if the path identifies a repository.
//
// Supported formats:
//   - /{org}/{project}/_git/{repo} (from HTTPS remote URLs)
//   - /v3/{org}/{project}/{repo} (from SSH remote URLs)
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
	if f.legacyOrganization != "" {
		path = "/" + f.legacyOrganization + "/" + strings.TrimPrefix(path, "/")
	}
	org, project, repo, err := extractRepoInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:          f.URL(),
		organization: org,
		project:      project,
		repository:   repo,
	}, nil
}

func legacyOrganization(remoteURL *giturl.URL) string {
	if remoteURL == nil {
		return ""
	}
	host := strings.ToLower(remoteURL.Hostname)
	organization, ok := strings.CutSuffix(host, ".visualstudio.com")
	if !ok || organization == "" || strings.Contains(organization, ".") {
		return ""
	}
	return organization
}

// OpenRepository opens the Azure DevOps repository
// that the given ID points to.
func (f *Forge) OpenRepository(
	ctx context.Context,
	tok forge.AuthenticationToken,
	id forge.RepositoryID,
) (forge.Repository, error) {
	rid := mustRepositoryID(id)
	adt := tok.(*AuthenticationToken)

	// For Azure CLI auth, refresh the token
	// by calling 'az account get-access-token'.
	// Azure CLI tokens expire after ~1 hour,
	// so we always fetch a fresh one.
	if adt.AuthType == AuthTypeAzureCLI {
		azExe, err := _execLookPath("az")
		if err != nil {
			return nil, fmt.Errorf(
				"refresh Azure CLI token: %w", err,
			)
		}

		freshToken, err := getAzureCLIToken(
			ctx, azExe, f.Execer,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"refresh Azure CLI token: %w", err,
			)
		}

		f.logger().Debug("Refreshed Azure CLI token")
		adt = &AuthenticationToken{
			AccessToken: freshToken,
			AuthType:    AuthTypeAzureCLI,
		}
	}

	// The Azure DevOps SDK expects an organization-scoped URL
	// (e.g. https://dev.azure.com/{org}),
	// not just the base URL.
	orgURL := f.URL() + "/" + rid.organization
	client, err := newAzureDevOpsClient(ctx, orgURL, adt)
	if err != nil {
		return nil, fmt.Errorf("create Azure DevOps client: %w", err)
	}

	return newRepository(ctx, f, rid, f.logger(), client)
}

// RepositoryID is a unique identifier for an Azure DevOps repository.
type RepositoryID struct {
	url          string // base URL (e.g., https://dev.azure.com)
	organization string // organization name
	project      string // project name
	repository   string // repository name
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

func mustRepositoryID(id forge.RepositoryID) *RepositoryID {
	rid, ok := id.(*RepositoryID)
	if ok {
		return rid
	}
	panic(fmt.Sprintf("expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return fmt.Sprintf("%s/%s/%s", rid.organization, rid.project, rid.repository)
}

// ChangeURL returns a URL to view a change on Azure DevOps.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	prID := mustPR(id).Number
	return fmt.Sprintf(
		"%s/%s/%s/_git/%s/pullrequest/%d",
		rid.url, rid.organization, rid.project, rid.repository, prID,
	)
}

// extractRepoInfo extracts organization, project, and repository names
// from an already host-matched Azure DevOps repository path.
//
// Supported formats:
//   - /{org}/{project}/_git/{repo} (from HTTPS remote URLs)
//   - /v3/{org}/{project}/{repo} (from SSH remote URLs)
func extractRepoInfo(path string) (org, project, repo string, err error) {
	s := strings.Trim(path, "/")
	s = strings.TrimSuffix(s, ".git")

	if rest, ok := strings.CutPrefix(s, "v3/"); ok {
		return extractSSHRepoInfo(rest)
	}

	return extractHTTPSRepoInfo(s)
}

func extractSSHRepoInfo(s string) (org, project, repo string, err error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf(
			"invalid Azure DevOps SSH URL format: expected v3/{org}/{project}/{repo}, got %q",
			s,
		)
	}

	return parts[0], parts[1], parts[2], nil
}

func extractHTTPSRepoInfo(s string) (org, project, repo string, err error) {
	// Path format: {org}/{project}/_git/{repo}
	parts := strings.Split(s, "/")
	if len(parts) < 4 {
		return "", "", "", fmt.Errorf(
			"invalid Azure DevOps URL path: expected {org}/{project}/_git/{repo}, got %q",
			s,
		)
	}

	// Find the _git segment.
	gitIdx := -1
	for i, p := range parts {
		if p == "_git" {
			gitIdx = i
			break
		}
	}

	if gitIdx < 2 || gitIdx >= len(parts)-1 {
		return "", "", "", fmt.Errorf(
			"invalid Azure DevOps URL path: missing _git segment in %q",
			s,
		)
	}

	org = parts[0]
	project = parts[gitIdx-1]
	repo = parts[gitIdx+1]

	return org, project, repo, nil
}
