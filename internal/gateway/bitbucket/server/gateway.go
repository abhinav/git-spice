package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"golang.org/x/mod/semver"
)

// errNoServerURL reports that an operation against
// a Bitbucket Data Center instance needs an instance URL,
// but none was configured or derived from the Git remote.
var errNoServerURL = errors.New(
	"no Bitbucket Data Center URL configured: " +
		"set spice.forge.bitbucket.url or BITBUCKET_URL, " +
		"or set spice.forge.kind=bitbucket to derive it from the Git remote",
)

const (
	// _draftMinVersion is the first Data Center version with draft pull requests.
	_draftMinVersion = "8.18.0"

	// _draftMinBuildNumber is the build number for _draftMinVersion (semver fallback).
	_draftMinBuildNumber = 8018000
)

// serverRepositoryID is a unique identifier
// for a Bitbucket Data Center repository.
type serverRepositoryID struct {
	url        string // required
	projectKey string // required
	slug       string // required

	// personal reports whether this is a personal ("~user") repository;
	// when true, projectKey holds the username.
	personal bool
}

// String returns a human-readable name for the repository ID.
func (rid *serverRepositoryID) String() string {
	if rid.personal {
		return fmt.Sprintf("~%s/%s", rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/%s", rid.projectKey, rid.slug)
}

// ChangeURL returns the web URL for a Pull Request
// hosted on Bitbucket Data Center.
func (rid *serverRepositoryID) ChangeURL(number int64) string {
	return fmt.Sprintf(
		"%s/pull-requests/%d/overview",
		rid.webBase(), number,
	)
}

// webBase returns the web URL prefix for the repository,
// up to and including the repository slug.
func (rid *serverRepositoryID) webBase() string {
	if rid.personal {
		return fmt.Sprintf("%s/users/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s", rid.url, rid.projectKey, rid.slug)
}

// restProjectKey returns the project key used in project-centric REST paths.
// Bitbucket Data Center expects personal repository REST paths
// to use the user's slug prefixed by tilde as the project key:
// https://confluence.atlassian.com/bitbucketserverkb/personal-projects-in-bitbucket-data-center-1072204133.html
func (rid *serverRepositoryID) restProjectKey() string {
	if rid.personal && !strings.HasPrefix(rid.projectKey, "~") {
		return "~" + rid.projectKey
	}
	return rid.projectKey
}

// Gateway implements [bitbucket.Gateway] for Bitbucket Data Center
// (REST 1.0) on top of the thin [Client].
type Gateway struct {
	client *Client
	repoID *serverRepositoryID
	log    *silog.Logger

	// repoNumericID memoizes the numeric repository ID; see numericRepoID.
	repoNumericMu sync.Mutex
	repoNumericID int64

	// draft* memoize the draft-support probe; see draftSupport.
	draftMu        sync.Mutex
	draftProbed    bool
	draftSupported bool
	draftKnown     bool
	draftVersion   string
}

var _ bitbucket.Gateway = (*Gateway)(nil)

// New builds the Bitbucket Data Center gateway
// for the repository {projectKey}/{slug}
// on the instance at baseURL,
// talking to the REST API rooted at apiURL.
//
// personal reports whether the repository
// is a personal ("~user") repository;
// when true, projectKey holds the username.
func New(
	apiURL, baseURL string,
	projectKey, slug string,
	personal bool,
	log *silog.Logger,
	token *Token,
) (*Gateway, error) {
	if baseURL == "" {
		return nil, errNoServerURL
	}

	if token == nil {
		return nil, errors.New(
			"build Bitbucket Data Center token source: nil authentication token",
		)
	}

	client, err := NewClient(
		StaticTokenSource(Token{
			AccessToken: token.AccessToken,
		}),
		&ClientOptions{BaseURL: apiURL},
	)
	if err != nil {
		return nil, fmt.Errorf("create Bitbucket Data Center client: %w", err)
	}

	return &Gateway{
		client: client,
		repoID: &serverRepositoryID{
			url:        baseURL,
			projectKey: projectKey,
			slug:       slug,
			personal:   personal,
		},
		log: log,
	}, nil
}

// Product returns the product name used in user-facing warnings.
func (*Gateway) Product() string { return "Bitbucket Data Center" }

// ChangeURL returns the web URL
// for viewing the pull request with the given number.
func (g *Gateway) ChangeURL(number int64) string {
	return g.repoID.ChangeURL(number)
}

// ChangeTemplate fetches the contents of the change template file
// at the given path on the repository's default branch.
//
// Returns an error matching [forge.ErrNotFound]
// if the file does not exist.
func (g *Gateway) ChangeTemplate(
	ctx context.Context,
	path string,
) (string, error) {
	body, _, err := g.client.RawFileGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, path,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf(
				"template %q not found: %w", path, forge.ErrNotFound,
			)
		}
		return "", err
	}
	return string(body), nil
}

// ListCommitChecks reports the CI checks
// recorded for the given commit.
// Any non-SUCCESSFUL, non-INPROGRESS state
// (including unrecognized ones) counts as failing.
func (g *Gateway) ListCommitChecks(
	ctx context.Context,
	commit git.Hash,
) ([]forge.ChangeCheck, error) {
	statuses, err := g.client.BuildStatusList(ctx, string(commit))
	if err != nil {
		return nil, fmt.Errorf("list build statuses: %w", err)
	}

	checks := make([]forge.ChangeCheck, 0, len(statuses))
	for i, s := range statuses {
		check := forge.ChangeCheck{Name: s.Key}
		if check.Name == "" {
			check.Name = fmt.Sprintf("Bitbucket build status %d", i+1)
		}
		switch s.State {
		case BuildStatusSuccessful:
			check.State = forge.ChangeCheckPassed
		case BuildStatusInProgress:
			check.State = forge.ChangeCheckPending
		default:
			check.State = forge.ChangeCheckFailed
		}
		checks = append(checks, check)
	}
	return checks, nil
}

// numericRepoID resolves and memoizes the numeric repository ID, which the
// default-reviewers endpoint requires but [serverRepositoryID] does not carry.
// Only a successful lookup is cached, so failures are retried.
func (g *Gateway) numericRepoID(ctx context.Context) (int64, error) {
	g.repoNumericMu.Lock()
	defer g.repoNumericMu.Unlock()
	if g.repoNumericID != 0 {
		return g.repoNumericID, nil
	}
	repo, _, err := g.client.RepositoryGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug,
	)
	if err != nil {
		return 0, fmt.Errorf("get repository: %w", err)
	}
	g.repoNumericID = repo.ID
	return g.repoNumericID, nil
}

// draftSupport reports whether the server supports draft pull requests.
// known is false when the version cannot be read; version is the raw
// reported version, for diagnostics.
//
// The probe is memoized:
// only a successful read of the server descriptor is cached,
// so transient failures are retried.
func (g *Gateway) draftSupport(
	ctx context.Context,
) (supported, known bool, version string) {
	g.draftMu.Lock()
	defer g.draftMu.Unlock()
	if g.draftProbed {
		return g.draftSupported, g.draftKnown, g.draftVersion
	}

	props, err := g.client.ApplicationProperties(ctx)
	if err != nil || props == nil {
		g.log.Debug("Could not read Bitbucket Data Center version; proceeding best-effort", "error", err)
		return false, false, ""
	}

	version = props.Version
	switch {
	case semver.IsValid("v" + props.Version):
		supported = semver.Compare("v"+props.Version, "v"+_draftMinVersion) >= 0
		known = true
	default:
		// Fall back to the build number when the version isn't valid semver.
		if n, err := strconv.Atoi(props.BuildNumber); err == nil && n > 0 {
			supported = n >= _draftMinBuildNumber
			known = true
		}
	}

	g.draftProbed = true
	g.draftSupported, g.draftKnown, g.draftVersion = supported, known, version
	return supported, known, version
}
