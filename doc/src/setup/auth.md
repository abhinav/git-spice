---
icon: material/lock
description: >-
  Authenticate with GitHub/GitLab to push and pull changes.
---

# Authentication

git-spice is offline-first.
It does not require authentication for local stacking operations.
However, once you want to push or pull changes to/from a remote repository,
you will need to authenticate with the respective service.

This page covers methods to authenticate git-spice with GitHub and GitLab.
Note that GitLab support requires at least version <!-- gs:version v0.9.0 -->.

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
    inside a Git repository cloned from GitHub or GitLab.

## Authentication methods

Each supported service supports different authentication methods.

- [OAuth](#oauth): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
- [GitHub App](#github-app): <!-- gs:badge:github -->
- [Personal Access Token](#personal-access-token): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
- [Service CLI](#service-cli): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->
- [Environment variable](#environment-variable): <!-- gs:badge:github --> <!-- gs:badge:gitlab -->

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

!!! note

    For private repositories owned by organizations,
    you will need a member with administrative access to the repository
    to allow installation of the git-spice GitHub App.
    If that is not an option,
    use a [Personal Access Token](#personal-access-token).

### Personal Access Token

**Supported by** <!-- gs:badge:github --> <!-- gs:badge:gitlab -->

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
              - select `repo` scope for full access to all repositories,
                or `public_repo` for access to public repositories only

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

          3. Click "Generate token" and copy the token.

=== "<!-- gs:gitlab -->"

    To use a Personal Access Token with GitLab:

    1. Go to <https://gitlab.com/-/user_settings/personal_access_tokens>.
    2. Select *Add new token*
    3. In the token creation form:

        - pick a descriptive name for the token
        - pick an expiration date if needed
        - select the `api` scope

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

**Supported by** <!-- gs:badge:github --> <!-- gs:badge:gitlab -->

You can provide the authentication token as an environment variable.
This is not recommended as a primary authentication method,
but it can be useful in CI/CD environments.

=== "<!-- gs:github -->"

    Set the `GITHUB_TOKEN` environment variable to your token.

=== "<!-- gs:gitlab -->"

    Set the `GITLAB_TOKEN` environment variable to your token.

If you have the environment variable set,
this takes precedence over all other authentication methods.

The $$gs auth login$$ operation will always fail if you use this method.

## Picking an authentication method

[OAuth](#oauth) is best if you have the permissions needed
to install it on all repositories that you want to use git-spice with.
Additionally, on GitHub, [GitHub App](#github-app) is similar,
but it may be preferable if you don't want to give git-spice
access to all your repositories.

[Service CLI](#service-cli) is the most convenient method if you already have
the CLI for the service installed and authenticated,
and your organization already allows its use.
It loses security benefits of the other methods,
as it re-uses the token assigned to the CLI.
For example, it you lose the ability to revoke the git-spice token
without revoking the CLI token.

[Personal Access Token](#personal-access-token) is flexible and secure.
It may be used even with repositories where you don't have permission to
install OAuth or GitHub Apps.
However, it requires manual token management, making it less convenient.

[Environment variable](#environment-variable) is the least convenient
and the least secure method. End users should typically never pick this.
It is intended only for CI/CD environments where you have no other choice.

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

<!-- gs:version unreleased -->
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
