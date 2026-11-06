package state

import "fmt"

const _versionFile = "version"

// Version specifies the version of the state store.
//
// It is stored in a 'version' file in the root of the store.
// Absence of this file indicates version 1.
type Version int

// Supported versions of the storage layout.
const (
	VersionOne Version = 1
	VersionTwo Version = 2

	// LatestVersion refers to the latest supported version.
	LatestVersion = VersionTwo
)

// checkVersion verifies that the given version is supported.
func checkVersion(version Version) error {
	// If/when we make a breaking change to the storage format,
	// we'll add migration code here.
	switch version {
	case VersionOne, VersionTwo:
		// ok

	default:
		return &VersionMismatchError{
			Want: LatestVersion,
			Got:  version,
		}
	}

	return nil
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
