package spice

import (
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/shamhub"
)

// NewTestService creates a new Service for testing.
// If forge is nil, it uses the ShamHub forge.
func NewTestService(
	repo GitRepository,
	store Store,
	forge forge.Forge,
	log *log.Logger,
) *Service {
	if forge == nil {
		forge = &shamhub.Forge{
			Log: log,
		}
	}

	return newService(repo, store, forge, log)
}
