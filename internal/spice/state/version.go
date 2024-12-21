package state

import (
	"context"
	"errors"
	"fmt"
)

const _versionFile = "version"

// Version specifies the version of the state store.
//
// It is stored in a 'version' file in the root of the store.
// Absence of this file indicates version 1.
type Version int

// Supported versions of the storage layout.
const (
	VersionOne Version = 1

	// LatestVersion refers to the latest supported version.
	LatestVersion = VersionOne
)

// checkVersion verifies that the given DB
// uses a supported version of the layout.
func checkVersion(ctx context.Context, db DB) error {
	version, err := loadVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("load store version: %w", err)
	}

	// If/when we make a breaking change to the storage format,
	// we'll add migration code here.
	switch version {
	case VersionOne:
		// ok

	default:
		return &VersionMismatchError{
			Want: LatestVersion,
			Got:  version,
		}
	}

	return nil
}

// loadVersion loads the version of the storage layout used by the given [DB].
func loadVersion(ctx context.Context, db DB) (Version, error) {
	var version Version

	if err := db.Get(ctx, _versionFile, &version); err != nil {
		if errors.Is(err, ErrNotExist) {
			// Version file was added during storage version 1.
			// If file does not exist, it's an old v1 store.
			return VersionOne, nil
		}

		return 0, err
	}

	return version, nil
}

// VersionMismatchError indicates that the data store we attempted to open
// is using a version older than this binary knows how to handle.
type VersionMismatchError struct {
	Want Version
	Got  Version
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("expected store version <= %d, got %d", e.Want, e.Got)
}
