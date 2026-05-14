package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationShowCmd struct{}

func (*integrationShowCmd) Help() string {
	return text.Dedent(`
		Displays the configured integration branch and the tip branches
		that compose it. For each tip, shows whether its hash has drifted
		from the hash recorded at the last successful rebuild.
	`)
}

func (cmd *integrationShowCmd) Run(
	ctx context.Context,
	kctx *kong.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	stdout := kctx.Stdout
	status, err := handler.Show(ctx)
	if err != nil {
		if errors.Is(err, integration.ErrNotConfigured) {
			log.Info("No integration branch configured.")
			log.Info("Run 'gs integration create <name>' to configure one.")
			return nil
		}
		return err
	}

	fmt.Fprintf(stdout, "Integration branch: %s\n", status.Name)
	if status.UpstreamBranch != "" && status.UpstreamBranch != status.Name {
		fmt.Fprintf(stdout, "Upstream branch: %s\n", status.UpstreamBranch)
	}
	if status.LastPushedHash != "" {
		fmt.Fprintf(stdout, "Last pushed: %s\n", status.LastPushedHash.Short())
	}

	if len(status.Tips) == 0 {
		fmt.Fprintln(stdout, "No tips configured.")
		return nil
	}

	fmt.Fprintln(stdout, "Tips:")
	for _, tip := range status.Tips {
		switch {
		case tip.Missing:
			fmt.Fprintf(stdout, "  - %s (missing)\n", tip.Name)
		case tip.StoredHash == "":
			fmt.Fprintf(stdout, "  - %s (pending rebuild)\n", tip.Name)
		case tip.Drifted():
			fmt.Fprintf(stdout, "  - %s (drifted: stored=%s current=%s)\n",
				tip.Name, tip.StoredHash.Short(), tip.CurrentHash.Short())
		default:
			fmt.Fprintf(stdout, "  - %s (%s)\n", tip.Name, tip.StoredHash.Short())
		}
	}

	return nil
}
