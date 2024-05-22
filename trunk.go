package gitspice

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
)

type trunkCmd struct{}

func (*trunkCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	trunk := store.Trunk()
	return (&branchCheckoutCmd{Name: trunk}).Run(ctx, log, opts)
}
