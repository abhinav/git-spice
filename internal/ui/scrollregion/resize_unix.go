//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || aix || zos

package scrollregion

import (
	"context"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"go.abhg.dev/gs/internal/sigstack"
)

func (m *Model) watchResize(
	ctx context.Context,
	program *tea.Program,
) <-chan struct{} {
	done := make(chan struct{})
	if _, _, ok := m.renderer.latestWindowSize(); !ok {
		close(done)
		return done
	}

	signals := make(chan sigstack.Signal, 1)
	// Register through sigstack so other terminal components can share
	// SIGWINCH without replacing each other's signal handlers.
	m.signals.Notify(signals, syscall.SIGWINCH)
	go func() {
		defer close(done)
		defer m.signals.Stop(signals)

		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				// SIGWINCH means the terminal may have changed size.
				// Read the size from the same output stream used for rendering,
				// then forward the Bubble Tea size message into the wrapped model.
				if width, height, ok := m.renderer.latestWindowSize(); ok {
					program.Send(tea.WindowSizeMsg{
						Width:  width,
						Height: height,
					})
				}
			}
		}
	}()
	return done
}
