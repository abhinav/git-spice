package main

import (
	"context"
	"errors"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type integrationSubmitCmd struct{}

func (*integrationSubmitCmd) Help() string {
	return text.Dedent(`
		Pushes the integration branch to the configured remote with
		--force-with-lease against the hash recorded at the previous
		successful push.

		No change request (PR) is opened: this command only pushes the
		branch. Once a manual submit succeeds, 'gs stack submit' and
		'gs upstack submit' will keep the published branch in sync with
		local rebuilds.
	`)
}

func (cmd *integrationSubmitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler IntegrationHandler,
) error {
	err := handler.Submit(ctx)
	if err == nil {
		log.Info("Integration branch pushed.")
		return nil
	}

	var rejected *integration.PushRejectedError
	if errors.As(err, &rejected) {
		log.Error(formatPushRejected(rejected))
		return err
	}
	return err
}

// formatPushRejected renders a multi-line explanation of a
// [*integration.PushRejectedError] tailored for the user.
func formatPushRejected(e *integration.PushRejectedError) string {
	var b strings.Builder
	b.WriteString("Cannot push integration branch:\n")
	b.WriteString("  remote ")
	b.WriteString(e.Remote)
	b.WriteString("/")
	b.WriteString(e.UpstreamBranch)
	b.WriteString(" is at ")
	b.WriteString(e.RemoteHash.Short())
	b.WriteString("\n  local ")
	b.WriteString(e.Branch)
	b.WriteString(" would push ")
	b.WriteString(e.LocalHash.Short())
	b.WriteString("\n  no previously-pushed hash is recorded for this checkout\n\n")
	b.WriteString("The integration branch is a local throwaway artifact; ")
	b.WriteString("'git pull' is NOT the right move.\n\n")
	b.WriteString("Likely causes:\n")
	b.WriteString("  - You ran 'git push' directly, bypassing gs's tracking.\n")
	b.WriteString("  - The same integration branch is being pushed from another checkout.\n")
	b.WriteString("  - The spice state was reset (fresh clone, manual ref edit, rebuild).\n\n")
	b.WriteString("To resolve, either accept the remote and overwrite on the next push:\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration mark-pushed\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration submit\n")
	b.WriteString("Or start over locally:\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration delete\n")
	b.WriteString("  ")
	b.WriteString(cli.Name())
	b.WriteString(" integration create ...\n\n")
	b.WriteString("If multiple checkouts are publishing this branch, stop. ")
	b.WriteString("It is inherently lossy.")
	return b.String()
}
