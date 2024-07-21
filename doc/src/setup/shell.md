---
icon: material/console-line
description: >-
  Set up shell completion for Bash, Zsh, and Fish.
---

# Shell completion

git-spice supports completion for Bash, Zsh, and Fish.
To set up completion, follow the instructions below.

=== "Bash"

    Add the following line to your `.bashrc` or `.bash_profile`:

    ```bash
    eval "$(gs shell completion bash)"
    ```

=== "Zsh"

    Add the following line to your `.zshrc` or `.zprofile`:

    ```zsh
    eval "$(gs shell completion zsh)"
    ```

=== "Fish"

    Add the following line to your `config.fish`:

    ```fish
    eval "$(gs shell completion fish)"
    ```

Then restart the shell or create a new shell session.

??? info "Debugging shell completion"

    If shell completion misbehaves and you want to file a bug report,
    first retry the completion with `COMP_DEBUG=1` set.

    ```freeze language="terminal"
    {green}${reset} {blue}export{reset} COMP_DEBUG=1
    {green}${reset} gs branch onto {gray}# <TAB>{reset}
    ```

    This will print debugging information about the completion process.
    Include this output in your bug report.
