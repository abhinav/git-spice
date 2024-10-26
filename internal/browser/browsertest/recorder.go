// Package browsertest provides test helpers
// for browser support.
package browsertest

import (
	"fmt"
	"os"

	"go.abhg.dev/gs/internal/browser"
)

// Recorder is a [Launcher] that records the URLs it is asked to open
// into a file.
type Recorder struct{ path string }

var _ browser.Launcher = (*Recorder)(nil)

// NewRecorder builds a [Recorder]
// that records URLs to the specified path.
func NewRecorder(path string) *Recorder {
	return &Recorder{path}
}

// OpenURL records the URL to the file.
func (r *Recorder) OpenURL(url string) error {
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = fmt.Fprintln(f, url)
	return err
}
