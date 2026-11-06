# ShamHub

ShamHub is a fake GitHub-like forge
used by git-spice tests and local demonstrations.
Normal git-spice builds must not include ShamHub.

Test scripts start ShamHub with `shamhub-setup`.
That command is a testscript-only lifecycle hook:
it starts the in-process ShamHub server,
sets `SHAMHUB_API_URL`, `SHAMHUB_URL`, and `SHAMHUB_ADMIN_TOKEN`,
and arranges server cleanup when the script exits.

All other `shamhub ...` commands in scripts run the same CLI
as local experimentation outside testscript.
The CLI talks to ShamHub administration endpoints
using `SHAMHUB_API_URL` and `SHAMHUB_ADMIN_TOKEN`.

For local experimentation,
start `shamhub-serve` as a long-lived process
and write its connection details to an environment file:

```sh
shamhub-serve --env-file /tmp/shamhub.env
```

In another shell,
source that file before running `shamhub`
or a git-spice binary built with ShamHub support:

```sh
. /tmp/shamhub.env
```

The server also starts an in-memory secret stash
and exports `SHAMHUB_SECRET_URL`.
Tagged git-spice builds use that URL
instead of the normal secret backend.

Build git-spice explicitly with ShamHub support:

```sh
go build -tags shamhub -o /tmp/demo/git-spice go.abhg.dev/gs
```

Do not add ShamHub to ordinary production build paths.
