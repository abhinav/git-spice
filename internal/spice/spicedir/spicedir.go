// Package spicedir owns the on-disk layout for git-spice's
// per-repository scratch directory: <repo-root>/.spice/.
//
// Today the directory holds persistent script-resolution files used by
// the message generator and the two auto-resolve features. Future
// features that need a per-repo cache (workspace snapshots, plan
// scratch, etc.) should put their data here instead of cluttering the
// repo root.
//
// Whether to track .spice/ in version control is the user's choice.
// spicedir does not modify .gitignore.
package spicedir

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the directory name relative to the repository root.
const DirName = ".spice"

// Path returns the absolute path to the spice directory inside the
// given repository root. Path does not create the directory; call
// [EnsureDir] for that.
func Path(repoRoot string) string {
	return filepath.Join(repoRoot, DirName)
}

// EnsureDir creates the spice directory (and any missing parents)
// under the given repository root. Returns nil if it already exists.
func EnsureDir(repoRoot string) error {
	dir := Path(repoRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create spice dir: %w", err)
	}
	return nil
}

// ResolutionPath returns the canonical path for a feature's
// resolution-history JSON file: <repo-root>/.spice/resolutions/<feature>.json.
//
// Callers should call [EnsureResolutionsDir] before writing to the
// returned path.
func ResolutionPath(repoRoot, feature string) string {
	return filepath.Join(Path(repoRoot), "resolutions", feature+".json")
}

// EnsureResolutionsDir creates <repo-root>/.spice/resolutions/ if it
// does not already exist.
func EnsureResolutionsDir(repoRoot string) error {
	dir := filepath.Join(Path(repoRoot), "resolutions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create resolutions dir: %w", err)
	}
	return nil
}
