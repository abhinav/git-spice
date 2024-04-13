package osutil

import (
	"errors"
	"os"
)

// TempFilePath creates a temporary file in dir with the given pattern
// and returns its path.
// If dir is an empty string, os.TempDir() is used.
//
// It is the caller's responsibility to delete the file when done.
func TempFilePath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}

	name := f.Name()
	if err := f.Close(); err != nil {
		return "", errors.Join(err, os.Remove(name))
	}

	return name, nil
}
