// Package forge provides an abstraction layer between git-spice
// and the underlying forge (e.g. GitHub, GitLab, Bitbucket).
package forge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"go.abhg.dev/gs/internal/git"
)

var _forgeRegistry sync.Map

// Register registers a forge with the given ID.
// Returns a function to unregister the forge.
func Register(f Forge) (unregister func()) {
	id := f.ID()
	_forgeRegistry.Store(id, f)
	return func() {
		_forgeRegistry.Delete(id)
	}
}

// OpenRepositoryURL opens a repository hosted on a forge
// by parsing the given remote URL.
//
// It will attempt to match the URL with all registered forges.
func OpenRepositoryURL(ctx context.Context, remoteURL string) (repo Repository, _ error) {
	var (
		attempted []string
		outerErr  error
	)
	_forgeRegistry.Range(func(key, value any) (keepGoing bool) {
		id := key.(string)
		forge := value.(Forge)

		var err error
		repo, err = forge.OpenURL(ctx, remoteURL)
		if err == nil {
			return false
		}

		if errors.Is(err, ErrUnsupportedURL) {
			attempted = append(attempted, id)
			return true
		}

		outerErr = fmt.Errorf("%v: %w", id, err)
		return false
	})

	if outerErr != nil {
		return nil, outerErr
	}

	if repo == nil {
		sort.Strings(attempted)
		return nil, fmt.Errorf("%w; attempted: %v", ErrUnsupportedURL, attempted)
	}

	return repo, nil
}

// ChangeID is a unique identifier for a change in a repository.
type ChangeID int

// TODO: ChangeID will become an interface in the future.

func (id ChangeID) String() string {
	return "#" + strconv.Itoa(int(id))
}

// ErrUnsupportedURL is returned when the given URL is not a valid GitHub URL.
var ErrUnsupportedURL = errors.New("unsupported URL")

// Forge is a forge that hosts Git repositories.
type Forge interface {
	// ID reports a unique identifier for the forge, e.g. "github".
	ID() string
	// TODO: Use ID as the storage key.

	// OpenURL opens a repository hosted on the forge
	// with the given remote URL.
	//
	// Returns [ErrUnsupportedURL] if the URL is not supported.
	OpenURL(ctx context.Context, remoteURL string) (Repository, error)
}

// Repository is a Git repository hosted on a forge.
type Repository interface {
	SubmitChange(ctx context.Context, req SubmitChangeRequest) (SubmitChangeResult, error)
	EditChange(ctx context.Context, id ChangeID, opts EditChangeOptions) error
	FindChangesByBranch(ctx context.Context, branch string) ([]*FindChangeItem, error)
	FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error)
	IsMerged(ctx context.Context, id ChangeID) (bool, error)

	// ListChangeTemplates returns templates defined in the repository
	// for new change proposals.
	//
	// Returns an empty list if no templates are found.
	ListChangeTemplates(context.Context) ([]*ChangeTemplate, error)
}

// SubmitChangeRequest is a request to submit a new change in a repository.
// The change must have already been pushed to the remote.
type SubmitChangeRequest struct {
	// Subject is the title of the change.
	Subject string // required

	// Body is the description of the change.
	Body string

	// Base is the name of the base branch
	// that this change is proposed against.
	Base string // required

	// Head is the name of the branch containing the change.
	//
	// This must have already been pushed to the remote.
	Head string // required

	// Draft specifies whether the change should be marked as a draft.
	Draft bool
}

// SubmitChangeResult is the result of creating a new change in a repository.
type SubmitChangeResult struct {
	ID  ChangeID
	URL string
}

// EditChangeOptions specifies options for an operation to edit
// an existing change.
type EditChangeOptions struct {
	// Base specifies the name of the base branch.
	//
	// If unset, the base branch is not changed.
	Base string

	// Draft specifies whether the change should be marked as a draft.
	// If unset, the draft status is not changed.
	Draft *bool
}

// FindChangeItem is a single result from searching for changes in the
// repository.
type FindChangeItem struct {
	// ID is a unique identifier for the change.
	ID ChangeID

	// URL is the web URL at which the change can be viewed.
	URL string

	// Subject is the title of the change.
	Subject string

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash

	// BaseName is the name of the base branch
	// that this change is proposed against.
	BaseName string

	// Draft is true if the change is not yet ready to be reviewed.
	Draft bool
}

// ChangeTemplate is a template for a new change proposal.
type ChangeTemplate struct {
	// Filename is the name of the template file.
	//
	// This is NOT a path.
	Filename string

	// Body is the content of the template file.
	Body string
}
