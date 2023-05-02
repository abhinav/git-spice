package osutil

import (
	"errors"
	"os"
)

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
