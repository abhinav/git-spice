package shamhub

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// CommandEnvironment returns ShamHub-specific variables for command hooks.
func (r *forgeRepository) CommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_SHAMHUB_CHANGE_NUMBER": strconv.Itoa(int(id.(ChangeID))),
		"GIT_SPICE_SHAMHUB_API_URL":       r.apiURL.String(),
		"GIT_SPICE_SHAMHUB_TOKEN":         r.client.headers["Authentication-Token"],
	}, nil
}
