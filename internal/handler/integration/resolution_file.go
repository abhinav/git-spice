package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"go.abhg.dev/gs/internal/scriptrun"
)

// ResolutionFeatureName is the feature key used in the resolution
// file path: .spice/resolutions/integration.json.
const ResolutionFeatureName = "integration"

// MergePair identifies a single in-progress merge by branch names.
type MergePair struct {
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
}

// ResolutionEntry holds the accumulated resolution instructions for a
// specific (ours, theirs) pair.
type ResolutionEntry struct {
	MergingBranches        MergePair          `json:"merging_branches"`
	ResolutionInstructions []scriptrun.QAPair `json:"resolution_instructions"`
}

// ResolutionFile is the on-disk format of the resolution state file.
//
// CurrentMerge is a transient pointer set by gs before each resolver
// invocation. Resolutions is the persistent history.
type ResolutionFile struct {
	CurrentMerge *MergePair        `json:"current_merge,omitempty"`
	Resolutions  []ResolutionEntry `json:"resolutions"`
}

// LoadResolutionFile reads the resolution file at path. If the file
// does not exist, returns an empty ResolutionFile (not an error).
//
// Returns an error if the file exists but cannot be read or parsed.
func LoadResolutionFile(path string) (*ResolutionFile, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &ResolutionFile{Resolutions: []ResolutionEntry{}}, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var f ResolutionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Resolutions == nil {
		f.Resolutions = []ResolutionEntry{}
	}
	return &f, nil
}

// Save writes the resolution file to path atomically: contents are
// written to a temporary file in the same directory and then renamed
// into place. The parent directory is created if missing.
func (f *ResolutionFile) Save(path string) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "integration-resolution.*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// EntryFor returns the existing entry matching p, or nil if absent.
func (f *ResolutionFile) EntryFor(p MergePair) *ResolutionEntry {
	for i := range f.Resolutions {
		if f.Resolutions[i].MergingBranches == p {
			return &f.Resolutions[i]
		}
	}
	return nil
}

// EnsureEntry returns the entry matching p, creating one with an empty
// instruction list if it does not already exist.
func (f *ResolutionFile) EnsureEntry(p MergePair) *ResolutionEntry {
	if e := f.EntryFor(p); e != nil {
		return e
	}
	f.Resolutions = append(f.Resolutions, ResolutionEntry{
		MergingBranches:        p,
		ResolutionInstructions: []scriptrun.QAPair{},
	})
	return &f.Resolutions[len(f.Resolutions)-1]
}

// AppendInstructions appends Q&A pairs to the entry matching p. The
// entry is created if it does not exist.
func (f *ResolutionFile) AppendInstructions(p MergePair, qa ...scriptrun.QAPair) {
	e := f.EnsureEntry(p)
	e.ResolutionInstructions = append(e.ResolutionInstructions, qa...)
}

// PruneBranch removes every entry where either side of merging_branches
// equals name. Returns the number of entries removed.
func (f *ResolutionFile) PruneBranch(name string) int {
	before := len(f.Resolutions)
	f.Resolutions = slices.DeleteFunc(f.Resolutions,
		func(e ResolutionEntry) bool {
			return e.MergingBranches.Ours == name ||
				e.MergingBranches.Theirs == name
		})
	return before - len(f.Resolutions)
}

// PruneStale removes every entry that references a branch not present
// in the provided set. Returns the number of entries removed.
func (f *ResolutionFile) PruneStale(tracked map[string]struct{}) int {
	before := len(f.Resolutions)
	f.Resolutions = slices.DeleteFunc(f.Resolutions,
		func(e ResolutionEntry) bool {
			_, oursOK := tracked[e.MergingBranches.Ours]
			_, theirsOK := tracked[e.MergingBranches.Theirs]
			return !oursOK || !theirsOK
		})
	return before - len(f.Resolutions)
}
