---
icon: material/lock
description: >-
  Authenticate with GitHub/GitLab/Bitbucket/Gitea/Forgejo
  to push and pull changes.
---

# Authentication

git-spice is offline-first.
It does not require authentication for local stacking operations.
However, once you want to push or pull changes to/from a remote repository,
you will need to authenticate with the respective service.

This page covers methods to authenticate git-spice
with GitHub, GitLab, Bitbucket Cloud,
Bitbucket Data Center / Server, Gitea, and Forgejo.
Note that GitLab support requires at least version <!-- gs:version v0.9.0 -->.
Bitbucket Cloud support requires at least version <!-- gs:version v0.25.0 -->.
Bitbucket Data Center / Server support requires at least version <!-- gs:version v0.31.0 -->.
Gitea support requires at least version <!-- gs:version v0.30.0 -->.
Forgejo support requires at least version <!-- gs:version v0.30.0 -->,
and defaults to Codeberg.

## Logging in

Take the following steps to authenticate with a service:

1. Run the following command:

    ```sh
    gs auth login
    ```

2. Pick the service you want to authenticate with.

    ```freeze language="ansi"
    --8<-- "captures/forge-prompt.txt"
    ```

3. You will be presented with a list of authentication methods.
   Pick the one that suits you best.

!!! tip

    Skip prompt (2) by running $$gs auth login$$
    inside a Git repository cloned from
    GitHub, GitLab, Bitbucket, Gitea, or Forgejo.
    For self-hosted Bitbucket,
    configure the instance first as described in
    [Bitbucket Data Center / Server](#bitbucket-data-center-server).

## Authentication methods

Each supported service supports different authentication methods.

- [OAuth](#oauth): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
- [GitHub App](#github-app): <!-- gs:badge:github -->
- [Git Credential Manager](#git-credential-manager): <!-- gs:badge:github --> <!-- gs:badge:bitbucket -->
- [Personal Access Token](#personal-access-token):
  <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
  <!-- gs:badge:bitbucket --> <!-- gs:badge:bitbucket-server -->
  <!-- gs:badge:gitea -->
  <!-- gs:badge:forgejo -->
- [Service CLI](#service-cli): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
- [Environment variable](#environment-variable):
  <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
  <!-- gs:badge:bitbucket --> <!-- gs:badge:bitbucket-server -->
  <!-- gs:badge:gitea -->
  <!-- gs:badge:forgejo -->

Read on for more details on each method,
or skip on to [Pick an authentication method](#picking-an-authentication-method).

### OAuth

**Supported by** <!-- gs:badge:github --> <!-- gs:badge:gitlab -->

With OAuth authentication, you will take the following steps:

1. Authenticate yourself on the service website in your browser.
2. Authorize git-spice to act on your behalf on the **current device only**.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}OAuth{reset}
  {gray}1.{reset} Visit {cyan}https://github.com/login/device{reset}
  {gray}2.{reset} Enter code: ABCD-1234
The code expires in a few minutes.
It will take a few seconds to verify after you enter it.
```

=== "<!-- gs:github -->"

    On GitHub, OAuth is available in two flavors:

    - **OAuth**: grants access to all repositories, public and private.
    - **OAuth: Public repositories only**:
      grants access to public repositories only.

    For more granular control than that,
    use [GitHub App](#github-app) authentication.

    !!! note

        For private repositories owned by organizations,
        you will need a member with administrative access to the repository
        to allow installation of the git-spice OAuth App.

        If that is not an option,
        use a [Personal Access Token](#personal-access-token).

=== "<!-- gs:gitlab -->"

    For Self-Hosted GitLab instances,
    an administrator will need to set up a git-spice OAuth App.
    Be sure to **uncheck** the "Confidential" option when creating the App.

    If that is not an option,
    use a [Personal Access Token](#personal-access-token).

### GitHub App

**Supported by** <!-- gs:badge:github -->

With GitHub App authentication, you will take the following steps:

1. Authenticate yourself on github.com in your browser.
2. Authorize git-spice to act on your behalf on the **current device only**.
3. Install the [git-spice GitHub App](https://github.com/apps/git-spice)
   on the repositories you want to use git-spice with.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}GitHub App{reset}
  {gray}1.{reset} Visit {cyan}https://github.com/login/device{reset}
  {gray}2.{reset} Enter code: ABCD-1234
The code expires in a few minutes.
It will take a few seconds to verify after you enter it.
```

**Important**: Authentication alone does not grant any access.
You **must** install the GitHub App to access repositories with git-spice.
GitHub App authentication is incompatible with Fork mode.
For GitHub fork workflows,
use one of the other GitHub authentication methods instead.

!!! note

    For private repositories owned by organizations,
    you will need a member with administrative access to the repository
    to allow installation of the git-spice GitHub App.
    If that is not an option,
    use a [Personal Access Token](#personal-access-token).

### Git Credential Manager

**Supported by** <!-- gs:badge:github --> <!-- gs:badge:bitbucket -->

[Git Credential Manager](https://github.com/git-credential-manager/git-credential-manager)
(GCM) is a secure credential storage system for Git.
If you already have GCM configured for GitHub or Bitbucket,
git-spice can reuse those credentials.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}Git Credential Manager{reset}
{green}INF{reset} successfully logged in
```

To set up GCM:

1. Install Git Credential Manager:

    === "macOS"

        ```sh
        brew install git-credential-manager
        ```

    === "Linux"

        Follow the instructions at
        <https://github.com/git-credential-manager/git-credential-manager>

2. Configure Git to use GCM:

    ```sh
    git config --global credential.helper manager
    ```

3. Push or pull from a GitHub or Bitbucket repository once.
   This triggers the OAuth flow in your browser.

After that, git-spice will use the stored OAuth token automatically.

### Personal Access Token

**Supported by**
<!-- gs:badge:github --> <!-- gs:badge:gitlab -->
<!-- gs:badge:bitbucket --> <!-- gs:badge:bitbucket-server -->
<!-- gs:badge:gitea --> <!-- gs:badge:forgejo -->

To use a Personal Access Token with git-spice,
you will generate a Personal Access Token on the website
and enter it in the prompt.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}Personal Access Token{reset}
{green}Enter Personal Access Token{reset}:
```

=== "<!-- gs:github -->"

    The token may be a classic token or a fine-grained token.

    === "Classic token"

        With classic tokens, you can grant access to all repositories,
        or all public repositories only.
        These tokens have the ability to never expire.

          To use a classic token:

          1. Go to <https://github.com/settings/tokens/new>.
             This may ask you to re-authenticate.
          2. In the token creation form:

              - enter a descriptive note for the token
              - pick an expiration window, or select "No expiration"
              - select the following scopes:
                `repo`, `read:org` for full access to all repositories,
                or just `public_repo` for access to public repositories only

          3. Click "Generate token" and copy the token.

    === "Fine-grained token"

        With fine-grained tokens, you have more granular control over
        repositories that you grant access to.
        These token must always have an expiration date.

          To use a fine-grained token:

          1. Go to <https://github.com/settings/personal-access-tokens/new>.
             This may ask you to re-authenticate.
          2. In the token creation form:

              - pick a descriptive note for the token
              - pick an expiration window
              - in the *Repository access* section, select the repositories
                you want to use git-spice with
              - in the *Repository permissions* section,
                grant **Read and write** access to **Pull requests** and **Contents**
              - (<!-- gs:version v0.21.0 --> or higher)
                in the *Organization permissions* section,
                grant **Read-only** access to **Members**

          3. Click "Generate token" and copy the token.

=== "<!-- gs:gitlab -->"

    To use a Personal Access Token with GitLab:

    1. Go to <https://gitlab.com/-/user_settings/personal_access_tokens>.
    2. Select *Add new token*
    3. In the token creation form:

        - pick a descriptive name for the token
        - pick an expiration date if needed
        - select the `api` scope

=== "<!-- gs:bitbucket -->"

    Bitbucket calls these **API tokens**,
    but they serve the same role as a Personal Access Token.

    1. Go to <https://bitbucket.org/account/settings/api-tokens/>.
    2. Click "Create token".
    3. Enter a descriptive label.
    4. Select the following scopes:

        - **pullrequest:write** - create and edit pull requests
        - **account** - read workspace members for reviewer lookup

    5. Click "Create" and copy the generated token.

    In the login prompt, this method may still appear as `API Token`.
    git-spice will then ask for your Atlassian account email and token:

    ```freeze language="terminal"
    {green}${reset} gs auth login
    Select an authentication method: {red}API Token{reset}
    {green}Enter Atlassian account email{reset}: user@example.com
    {green}Enter API token{reset}:
    {green}INF{reset} bitbucket: successfully logged in
    ```

=== "<!-- gs:bitbucket-server -->"

    Bitbucket Data Center / Server uses a Personal Access Token
    (an HTTP access token) as its only authentication method.
    Before logging in, tell git-spice how to find your instance
    (see [Self-hosted instances](#bitbucket-data-center-server) below).

    1. Go to your instance's
       *HTTP access tokens* page under your account settings,
       typically at `<instance-url>/plugins/servlet/access-tokens/manage`.
    2. Create a token with **Repository Write** permission.
    3. Copy the generated token.

    Then log in, specifying the forge explicitly:

    ```freeze language="terminal"
    {green}${reset} gs auth login {green}--forge {red}bitbucket{reset}
    {green}Enter HTTP access token{reset}:
    {green}INF{reset} bitbucket: successfully logged in
    ```

=== "<!-- gs:gitea -->"

    To use an API token with Gitea:

    1. Open your user settings on the Gitea instance.
       For the default Gitea UI,
       go to `{your Gitea instance}/user/settings/applications`.
    2. Create a new token.
    3. Grant the token repository access sufficient
       to create and update Pull Requests.
    4. Copy the generated token.

=== "<!-- gs:forgejo -->"

    To use an API token with Forgejo or Codeberg:

    1. Open your user settings on the Forgejo instance.
       On Codeberg,
       go to <https://codeberg.org/user/settings/applications>.
    2. Create a new token.
    3. Grant the token repository access sufficient
       to create and update Pull Requests.
    4. Copy the generated token.

After you have a token, enter it into the prompt.

### Service CLI

**Supported by** <!-- gs:badge:github --> <!-- gs:badge:gitlab -->

If you have the GitHub or GitLab CLIs installed and authenticated,
you can get authentication tokens for git-spice from them.

=== "<!-- gs:github -->"

    1. Install the [GitHub CLI](https://github.com/cli/cli#installation)
    2. Authenticate it:

        ```freeze language="terminal"
        {green}${reset} gh auth login
        ```

=== "<!-- gs:gitlab -->"

    1. Install the [GitLab CLI](https://gitlab.com/gitlab-org/cli#installation).
    2. Authenticate it:

        ```freeze language="terminal"
        {green}${reset} glab auth login
        ```

Once you pick this authentication option, no additional steps are required.
git-spice will request a token from the CLI as needed.

### Environment variable

**Supported by**
<!-- gs:badge:github --> <!-- gs:badge:gitlab -->
<!-- gs:badge:bitbucket --> <!-- gs:badge:bitbucket-server -->
<!-- gs:badge:gitea --> <!-- gs:badge:forgejo -->

You can provide the authentication token as an environment variable.
This is not recommended as a primary authentication method,
but it can be useful in CI/CD environments.

=== "<!-- gs:github -->"

    Set the `GITHUB_TOKEN` environment variable to your token.

=== "<!-- gs:gitlab -->"

    Set the `GITLAB_TOKEN` environment variable to your token.

=== "<!-- gs:bitbucket -->"

    Set the `BITBUCKET_TOKEN` environment variable to your OAuth token.
    This should be a Bearer token (OAuth access token).

=== "<!-- gs:bitbucket-server -->"

    Set the `BITBUCKET_TOKEN` environment variable
    to your HTTP access token (Personal Access Token).
    This is sent as a Bearer token.

    The same variable serves Bitbucket Cloud or Data Center,
    whichever the current repository resolves to;
    to hold tokens for several instances at once,
    log in with $$gs auth login$$ instead.

=== "<!-- gs:gitea -->"

    Set the `GITEA_TOKEN` environment variable to your API token.

=== "<!-- gs:forgejo -->"

    Set the `FORGEJO_TOKEN` environment variable to your API token.

If you have the environment variable set,
this takes precedence over all other authentication methods.

The $$gs auth login$$ operation will always fail if you use this method.

## Picking an authentication method

=== "<!-- gs:github -->"

    [OAuth](#oauth) is best if you have the permissions needed
    to install it on all repositories that you want to use git-spice with.

    [GitHub App](#github-app) is similar,
    but it may be preferable if you don't want to give git-spice
    access to all your repositories.
    GitHub App authentication is incompatible with Fork mode;
    use one of the other GitHub authentication methods instead.

    [Git Credential Manager](#git-credential-manager)
    is convenient if you already have GCM installed for git operations.

    [Service CLI](#service-cli) is the most convenient method
    if you already have the GitHub CLI installed and authenticated.
    It loses security benefits of the other methods,
    as it re-uses the token assigned to the CLI.

    [Personal Access Token](#personal-access-token)
    is flexible and secure.
    It may be used even with repositories where you don't have
    permission to install OAuth or GitHub Apps.
    However, it requires manual token management,
    making it less convenient.

=== "<!-- gs:gitlab -->"

    [OAuth](#oauth) is best if you have the permissions needed
    to install it on all repositories that you want to use git-spice with.

    [Service CLI](#service-cli) is the most convenient method if you already have
    the GitLab CLI installed and authenticated.
    It loses security benefits of the other methods,
    as it re-uses the token assigned to the CLI.

    [Personal Access Token](#personal-access-token) is flexible and secure.
    It may be used even with repositories where you don't have permission to
    install OAuth Apps.
    However, it requires manual token management, making it less convenient.

=== "<!-- gs:bitbucket -->"

    [Git Credential Manager](#git-credential-manager) integrates with
    Bitbucket's OAuth flow and handles token refresh automatically.
    This is convenient if you already have GCM installed for git operations.

    [Personal Access Token](#personal-access-token) is flexible and secure.
    It requires manual token management but works without additional tools.

=== "<!-- gs:bitbucket-server -->"

    [Personal Access Token](#personal-access-token) is the only
    interactive authentication method for Bitbucket Data Center / Server.
    It requires manual token management but works without additional tools.

=== "<!-- gs:gitea -->"

    [Personal Access Token](#personal-access-token) is the primary
    authentication method for Gitea.
    It requires manual token management but works without additional tools.

=== "<!-- gs:forgejo -->"

    [Personal Access Token](#personal-access-token) is the primary
    authentication method for Forgejo and Codeberg.
    It requires manual token management but works without additional tools.

[Environment variable](#environment-variable) is the least convenient
and the least secure method. End users should typically never pick this.
It is intended only for CI/CD environments where you have no other choice.

## Aliased SSH remotes

<!-- gs:version v0.30.0 -->

git-spice usually identifies the forge from the Git remote URL.
For example, a remote hosted on `github.com` identifies GitHub.

If your SSH configuration uses a host alias,
the remote URL may not identify the forge:

```freeze language="terminal"
{green}${reset} git remote -v
origin  git@github-work:OWNER/REPO.git (fetch)
origin  git@github-work:OWNER/REPO.git (push)
```

In that case, configure the forge kind explicitly:

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.kind{reset} {mag}github{reset}
```

After this, run $$gs auth login$$ and other forge-backed commands normally.
The configured kind selects the forge implementation;
the configured forge URL,
such as $$spice.forge.github.url$$ for GitHub Enterprise,
still controls web links and API requests.
For self-hosted Bitbucket instances,
an aliased remote also hides the real instance URL,
so set $$spice.forge.bitbucket.url$$ alongside the kind.

You may also set the same value for one command with the
`GIT_SPICE_FORGE_KIND` environment variable.

## Self-hosted instances

### GitHub Enterprise

To use git-spice with a GitHub Enterprise instance,
inform it of the instance URL, authenticate, and use git-spice as usual.

=== "<!-- gs:version v0.4.0 -->"

    Set the $$spice.forge.github.url$$ configuration option
    to the address of your GitHub Enterprise instance.

    ```freeze language="terminal"
    {green}${reset} git config {red}spice.forge.github.url{reset} {mag}https://github.example.com{reset}
    ```

    **Optionally**, also set the GitHub API URL
    with the $$spice.forge.github.apiUrl$$ configuration option.
    By default, the API URL is assumed to be at `/api` under the GitHub URL.

    ```freeze language="terminal"
    {green}${reset} git config {red}spice.forge.github.apiUrl{reset} {mag}https://github.example.com/api{reset}
    ```

    The GitHub API URL will typically end with `/api`, not `/api/v3` or similar.

    Alternatively, these configuration options may also be set
    with the `GITHUB_URL` and `GITHUB_API_URL` environment variables.

=== "<!-- gs:version v0.3.1 --> or older"

    Set the `GITHUB_URL` and `GITHUB_API_URL` environment variables
    to the address of your GitHub Enterprise instance
    and its API endpoint, respectively.

    These must both be set for git-spice to work with GitHub Enterprise.

```freeze language="bash"
export GITHUB_URL=https://github.example.com
export GITHUB_API_URL=https://github.example.com/api
```

### GitLab Self-Hosted

To use git-spice with a self-hosted GitLab instance,
set $$spice.forge.gitlab.url$$ to the address of your GitLab instance.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.gitlab.url{reset} {mag}https://gitlab.example.com{reset}
```

<!-- gs:version v0.13.0 -->
*Optionally*, also set the GitLab API URL
with the $$spice.forge.gitlab.apiUrl$$ configuration option.
By default, the API URL is the same as the GitLab URL.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.gitlab.apiUrl{reset} {mag}https://gitlab.example.com/api/v4{reset}
```

Alternatively, set these configuration options
with the `GITLAB_URL` and `GITLAB_API_URL` environment variables.

```freeze language="bash"
export GITLAB_URL=https://gitlab.example.com
export GITLAB_API_URL=https://gitlab-api.example.com
```

#### OAuth with GitLab Self-Hosted

To use OAuth authentication with a self-hosted GitLab instance,
you must first set up an OAuth App on the GitLab instance.
Be sure to **uncheck** the "Confidential" option when creating the App.
This will generate an OAuth Client ID for the App.

Feed that into git-spice with the $$spice.forge.gitlab.oauth.clientID$$
configuration option.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.gitlab.oauth.clientID{reset} {mag}your-client-id{reset}
```

This may also be set with the `GITLAB_OAUTH_CLIENT_ID` environment variable.

```freeze language="bash"
export GITLAB_OAUTH_CLIENT_ID=your-client-id
```

Authenticate with $$gs auth login$$ as usual after that.

### Bitbucket Data Center / Server

<!-- gs:version v0.31.0 -->

The same `bitbucket` forge serves Bitbucket Cloud
and self-hosted Bitbucket Data Center / Server instances.
Pick the recipe that matches your setup:

=== "Bitbucket Cloud"

    Repositories hosted on `bitbucket.org` need no configuration.
    Authenticate and use git-spice as usual.

=== "Self-hosted"

    In the repository you want to use git-spice with,
    set $$spice.forge.kind$$ to `bitbucket`.

    ```freeze language="terminal"
    {green}${reset} git config {red}spice.forge.kind{reset} {mag}bitbucket{reset}
    ```

    git-spice derives the instance URL from the repository's remote URL
    and selects the Bitbucket Data Center API automatically.
    Set this option before running $$gs auth login$$.

    !!! note

        For SSH remotes, the derived URL is `https://<host>`,
        without a custom web port or context path.
        If your instance's web UI is not served there,
        set $$spice.forge.bitbucket.url$$ explicitly instead.

=== "Explicit URL"

    If the derived URL is wrong, or you prefer to be explicit,
    set $$spice.forge.bitbucket.url$$ to the address of your instance
    in the repository you want to use git-spice with.

    ```freeze language="terminal"
    {green}${reset} git config {red}spice.forge.bitbucket.url{reset} {mag}https://bitbucket.example.com{reset}
    ```

    A URL other than `https://bitbucket.org` selects
    the Bitbucket Data Center API automatically.
    For exotic setups, two more options are available:

    - $$spice.forge.bitbucket.kind$$ overrides
      the Cloud-or-Data Center inference,
      e.g. set it to `cloud` to keep using the Cloud API
      behind a proxy URL.
    - $$spice.forge.bitbucket.apiURL$$ overrides the API URL.

git-spice resolves the instance per repository.
Each setting takes the first value that applies:

- **Instance URL**:
  $$spice.forge.bitbucket.url$$ if set;
  else derived from the remote URL when $$spice.forge.kind$$ is `bitbucket`;
  else `https://bitbucket.org`.
- **Kind**:
  $$spice.forge.bitbucket.kind$$ if set;
  else Cloud if the instance URL host is `bitbucket.org`
  or a subdomain of it;
  else Data Center.
- **API URL**:
  $$spice.forge.bitbucket.apiURL$$ if set;
  else `https://api.bitbucket.org/2.0` for Cloud,
  or the instance URL with `/rest/api/1.0` appended for Data Center.

After the configuration is set,
authenticate with a [Personal Access Token](#personal-access-token),
specifying the forge explicitly:

```freeze language="terminal"
{green}${reset} gs auth login {green}--forge {red}bitbucket{reset}
```

!!! warning

    A custom $$spice.forge.bitbucket.url$$ replaces `bitbucket.org`
    as the host that identifies the Bitbucket forge:
    if the option is set globally,
    repositories cloned from bitbucket.org no longer match it.
    Prefer setting the option per repository,
    as with a custom $$spice.forge.gitlab.url$$.

### Gitea

<!-- gs:version v0.30.0 -->

To use git-spice with a Gitea instance,
set $$spice.forge.gitea.url$$ to the address of your Gitea instance.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.gitea.url{reset} {mag}https://gitea.example.com{reset}
```

Alternatively, set the configuration with the `GITEA_URL` environment variable.

```freeze language="bash"
export GITEA_URL=https://gitea.example.com
```

Then authenticate with $$gs auth login$$.
Gitea supports Personal Access Tokens:

- **Personal Access Token**: Create one at
  `{your Gitea instance}/user/settings/applications`.
  Required scopes: `write:repository`, `write:issue`, and `read:user`.

Alternatively, set `GITEA_TOKEN` to skip the login flow:

```freeze language="bash"
export GITEA_TOKEN=your-gitea-token
```

### Forgejo

<!-- gs:version v0.30.0 -->

Codeberg is the default Forgejo host.
To use git-spice with a different Forgejo instance,
set $$spice.forge.forgejo.url$$ to the address of that instance.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.forgejo.url{reset} {mag}https://forgejo.example.com{reset}
```

*Optionally*, also set the Forgejo API URL
with the $$spice.forge.forgejo.apiURL$$ configuration option.
By default,
git-spice derives the API endpoint from the Forgejo URL.

```freeze language="terminal"
{green}${reset} git config {red}spice.forge.forgejo.apiURL{reset} {mag}https://forgejo.example.com/api/v1{reset}
```

Alternatively, set these configuration options
with the `FORGEJO_URL` and `FORGEJO_API_URL` environment variables.

```freeze language="bash"
export FORGEJO_URL=https://forgejo.example.com
export FORGEJO_API_URL=https://forgejo.example.com/api/v1
```

## Safety

By default, git-spice stores your authentication token
in a system-specific secure storage.
On macOS, this is the system Keychain.
On Linux, it uses the [Secret Service](https://specifications.freedesktop.org/secret-service/latest/),
which is typically provided by [GNOME Keyring](https://specifications.freedesktop.org/secret-service/latest/).
<!-- TODO (if we enable Windows): On Windows, it uses the Windows Credential Manager APIs. -->

Since version <!-- gs:version v0.3.0 -->,
if your system does not provide a secure storage service,
git-spice will fall back to storing secrets in a plain-text file
at `$XDG_CONFIG_HOME/git-spice/secrets.json` or the user's configuration directory.
If it does that, it will clearly indicate so at login time,
reporting the full path to the secrets file.

<details>
  <summary>Example</summary>

```freeze language="terminal"
{green}${reset} gs auth login
{gray}...{reset}
{yellow}WRN{reset} Storing secrets in plain text at /home/user/.config/git-spice/secrets.json. Be careful!
{green}INF{reset} github: successfully logged in
```

</details>

### Choosing a secret backend

<!-- gs:version v0.26.0 -->

You can override the default backend selection
with the $$spice.secret.backend$$ configuration option
or the `$GIT_SPICE_SECRET_BACKEND` environment variable.

For example,
to force file-based storage on a headless Linux machine:

```freeze language="bash"
export GIT_SPICE_SECRET_BACKEND=file
```

Or with git-config:

```freeze language="terminal"
{green}${reset} git config {red}spice.secret.backend{reset} {mag}file{reset}
```

See $$spice.secret.backend$$
for a full list of supported values.
