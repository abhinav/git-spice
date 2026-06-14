//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !aix && !zos

package scrollregion

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) watchResize(
	context.Context,
	*tea.Program,
) <-chan struct{} {
	// Bubble Tea does not provide SIGWINCH resize watching on these targets.
	done := make(chan struct{})
	close(done)
	return done
}
