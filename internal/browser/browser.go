// Package browser provides a means of opening a URL
// in the user's default web browser.
package browser

import (
	"github.com/cli/browser"
)

// Launcher launches the web browser.
type Launcher interface {
	// OpenURL opens the specified URL.
	OpenURL(url string) error
}

// Browser is a [Launcher] that opens URLs in a real web browser.
//
// Its zero value is a valid instance.
type Browser struct {
	openURL func(url string) error // to stub in tests
}

var _ Launcher = (*Browser)(nil)

// OpenURL opens the URL in the user's default web browser.
func (b *Browser) OpenURL(url string) error {
	openURL := browser.OpenURL
	if b.openURL != nil {
		openURL = b.openURL
	}
	return openURL(url)
}

// Noop is a [Launcher] that does nothing.
// Its zero value is a valid instance.
type Noop struct{}

var _ Launcher = (*Noop)(nil)

// OpenURL does nothing.
func (*Noop) OpenURL(string) error { return nil }
