package gittest

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Version is a Git version string.
type Version struct {
	Major, Minor, Patch int
}

// CurrentVersion returns the current Git version installed on the system.
func CurrentVersion() (Version, error) {
	raw, err := exec.Command("git", "--version").Output()
	if err != nil {
		return Version{}, fmt.Errorf("get Git version: %w", err)
	}
	version, err := ParseVersion(string(bytes.TrimSpace(raw)))
	if err != nil {
		return Version{}, fmt.Errorf("parse Git version output: %w", err)
	}
	return version, nil
}

// SkipUnlessVersionAtLeast skips the test unless the current Git version
// is at least the specified version.
func SkipUnlessVersionAtLeast(t testing.TB, want Version) {
	got, err := CurrentVersion()
	require.NoError(t, err, "get current Git version")

	if got.Compare(want) < 0 {
		t.Skipf("skipping test: Git version %v < %v", got, want)
	}
}

// ParseVersion parses a Git version string.
// This must be in one of the following formats:
//
//	git version X.Y.Z (...)
//	git version X.Y.Z
//	X.Y.Z
//	X.Y
//	X
//
// Where X, Y, and Z are integers.
func ParseVersion(orig string) (Version, error) {
	s := orig
	s = strings.TrimPrefix(s, "git version ")
	if i := strings.Index(s, " "); i >= 0 {
		s = s[:i] // "X.Y.Z (...)" => "X.Y.Z"
	}

	var (
		major, minor, patch int
		err                 error
	)
	switch toks := strings.Split(s, "."); len(toks) {
	case 3:
		patch, err = strconv.Atoi(toks[2])
		if err != nil {
			return Version{}, &badVersionPartError{orig, "patch", toks[2], err}
		}
		fallthrough

	case 2:
		minor, err = strconv.Atoi(toks[1])
		if err != nil {
			return Version{}, &badVersionPartError{orig, "minor", toks[1], err}
		}
		fallthrough

	case 1:
		major, err = strconv.Atoi(toks[0])
		if err != nil {
			return Version{}, &badVersionPartError{orig, "major", toks[0], err}
		}

	default:
		return Version{}, fmt.Errorf("bad version %q in %q: expected form X.Y.Z", s, orig)
	}

	return Version{major, minor, patch}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare compares two versions.
// It returns a negative value if v is less than other,
// zero if they are equal,
// and a positive value if v is greater than other.
func (v Version) Compare(other Version) int {
	switch {
	case v.Major != other.Major:
		return v.Major - other.Major
	case v.Minor != other.Minor:
		return v.Minor - other.Minor
	default:
		return v.Patch - other.Patch
	}
}

type badVersionPartError struct {
	Orig, Part, Value string
	Err               error
}

func (e *badVersionPartError) Error() string {
	return fmt.Sprintf("bad %s version %q in %q: %v", e.Part, e.Value, e.Orig, e.Err)
}
