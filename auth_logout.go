package main

import (
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type authLogoutCmd struct{}

func (*authLogoutCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		The stored authentication information is deleted.
		Use '%[1]s auth login' to log in again.

		Does not do anything if not logged in.
	`, name))
}

func (cmd *authLogoutCmd) Run(
	stash secret.Stash,
	log *silog.Logger,
	f forge.Forge,
) error {
	if err := f.ClearAuthenticationToken(stash); err != nil {
		return err
	}

	// TOOD: Forges should present friendly names in addition to IDs.
	log.Infof("%s: logged out", f.ID())
	return nil
}
