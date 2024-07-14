---
icon: material/lock
description: >-
  Authenticate with GitHub to push and pull changes.
---

# Authentication

git-spice is offline-first.
It does not require authentication for local stacking operations.
However, once you want to push or pull changes to/from a GitHub repository,
you will need to authenticate with GitHub.

This page covers the authentication options for git-spice.

## Logging in

To authenticate with GitHub, run:

```sh
gs auth login github
```

This will present you with a list of authentication methods.

See [Authentication methods](#authentication-methods) for details
on what to expect from each method,
or skip on to [Pick an authentication method](#picking-an-authentication-method).

## Authentication methods

git-spice provides several ways to authenticate with GitHub.

### OAuth

With OAuth authentication, you will take the following steps:

1. Authenticate yourself on github.com in your browser.
2. Authorize git-spice to act on your behalf on the **current device only**.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}OAuth{reset}
  {gray}1.{reset} Visit {cyan}https://github.com/login/device{reset}
  {gray}2.{reset} Enter code: ABCD-1234
The code expires in a few minutes.
It will take a few seconds to verify after you enter it.
```

Two options are available for OAuth:

- **OAuth**: grants access to all repositories, public and private.
- **OAuth: Public repositories only**:
  grants access to public repositories only.

For more granular control, use [GitHub App](#github-app) authentication.

!!! note

    For private repositories owned by organizations,
    you will need a member with administrative access to the repository
    to allow installation of the git-spice OAuth App.
    If that is not an option,
    use a [Personal Access Token](#personal-access-token).

### GitHub App

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

To use a Personal Access Token with git-spice,
you will generate a Personal Access Token on GitHub
and enter it in the prompt.

```freeze language="terminal"
{green}${reset} gs auth login
Select an authentication method: {red}Personal Access Token{reset}
{green}Enter Personal Access Token{reset}:
```

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

After you have a token, enter it into the prompt.

### GitHub CLI

If you have the [GitHub CLI](https://cli.github.com/) installed and authenticated,
you can select this as the authentication method.

This requires no additional steps.
We'll request a token from GitHub CLI as needed.

### GITHUB_TOKEN

If you have a `GITHUB_TOKEN` environment variable set,
that takes precedence over all other authentication methods.

The $$gs auth login$$ operation will fail if you use this method.

## Picking an authentication method

[OAuth](#oauth) and [GitHub App](#github-app) authentication are best if you
have the permissions needed to install OAuth/GitHub Apps
on all repositories that you want to use git-spice with.
The two are equivalent in terms of user experience.
Use GitHub App authentication if you don't want to give git-spice access
to all your repositories.

[GitHub CLI](#github-cli) is the most convenient method if you already have
the GitHub CLI installed and authenticated.
It loses some security benefits of the other methods,
as it re-uses the token from the GitHub CLI.
You lose the ability to revoke the git-spice token
without revoking the GitHub CLI token.

[Personal Access Token](#personal-access-token) is flexible and secure.
It may be used even with repositories where you don't have permission to
install OAuth/GitHub Apps.
However, it requires manual token management, making it less convenient.

[GITHUB_TOKEN](#github_token) is the least convenient and the least secure method.
It is intended only for CI/CD environments where you have no other choice.

!!! question "Where is my token stored?"

    The token is stored in a system-specific secure storage.
    See [Secret storage](../guide/internals.md#secret-storage) for details.
