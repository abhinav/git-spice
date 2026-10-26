package github

import "context"

// RefExists checks if a reference exists in the repository.
// ref must be a fully qualified reference name,
func (r *Repository) RefExists(ctx context.Context, ref string) (bool, error) {
	return r.gateway.RefExists(ctx, r.owner, r.repo, ref)
}
