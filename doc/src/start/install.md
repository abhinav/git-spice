---
icon: material/download
description: >-
  Install git-spice on your system and start using it.
next_page: stack.md
---

# Installation

git-spice may be installed with a pre-built binary
or built from source.

## Pre-built binary

To install a **pre-built binary**, take the following steps:

=== "Homebrew/Linuxbrew"

    ```bash
    brew install abhinav/tap/git-spice
    ```

=== "AUR (ArchLinux)"

    ```bash
    git clone https://aur.archlinux.org/git-spice-bin.git
    cd git-spice-bin
    makepkg -si

    # Or, with an AUR helper like yay:
    yay -S git-spice-bin
    ```

Pre-built binaries for other platforms can be found on the
[GitHub Releases page](https://github.com/abhinav/git-spice/releases).

## Build from source

To **build from source**, follow these steps:


1. Install the [Go compiler](https://go.dev/dl).
2. Run the following command:

    ```bash
    go install go.abhg.dev/gs@latest
    ```

## Next steps

- [ ] [Create your first stack](stack.md)
