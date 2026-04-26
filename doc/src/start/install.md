---
icon: material/download
description: >-
  Install git-spice on your system and start using it.
next_page: stack.md
---

# Installation

git-spice may be installed with a pre-built binary
or built from source.

!!! info "System requirements"

    - **Operating system**:
      Linux and macOS are fully supported.
      Windows support was added in <!-- gs:version v0.6.0 -->.

    - **Architecture**:
      x86_64 and aarch64 architectures are supported.
      Additionally on Linux, some 32-bit ARM architectures are also supported.

    - **Git**:
      At least Git 2.38 is required for git-spice to operate correctly.
      Earlier versions may work, but are not officially supported.

## Pre-built binary

To install a **pre-built binary**, use one of the following methods:

### Homebrew/Linuxbrew

git-spice is available in homebrew-core
(the default formulae repository for Homebrew and Linuxbrew).
Install git-spice with the following command:

```bash
brew install git-spice
```

You can also use my Homebrew Tap to install the latest release:

```bash
brew install --cask abhinav/tap/git-spice
```

### Binary installation tools

#### ubi

[ubi](https://github.com/houseabsolute/ubi) is a binary installation tool
that is able to download pre-built binaries from GitHub Releases.

If you use ubi, use the following command to install git-spice:

```bash
ubi --project abhinav/git-spice --exe git-spice
```

#### mise

[mise](https://mise.jdx.dev) supports installing tools from various sources,
including directly from GitHub.

If you use mise, use the following command to install git-spice:

```bash
mise use --global 'github:abhinav/git-spice'
```

### AUR (ArchLinux)

If you're using ArchLinux, install the 'git-spice-bin' package from the AUR:

```bash
git clone https://aur.archlinux.org/git-spice-bin.git
cd git-spice-bin
makepkg -si

# Or, with an AUR helper like yay:
yay -S git-spice-bin
```

### Linux packages

<!-- gs:version v0.27.0 -->

GitHub Releases also provides native Linux packages:

- `.deb` packages for Debian and Ubuntu
- `.rpm` packages for Fedora, RHEL, Rocky, AlmaLinux, and openSUSE
- `.apk` packages for Alpine

Download the package for your architecture
from the [GitHub Releases page](https://github.com/abhinav/git-spice/releases),
then install it with your system package manager:

=== "Debian / Ubuntu"

    ```bash
    sudo apt install ./git-spice_<version>_<arch>.deb
    ```

=== "Fedora / RHEL / Rocky / AlmaLinux"

    ```bash
    sudo dnf install ./git-spice-<version>-1.<arch>.rpm
    ```

=== "openSUSE"

    ```bash
    sudo zypper install ./git-spice-<version>-1.<arch>.rpm
    ```

=== "Alpine"

    ```bash
    sudo apk add --allow-untrusted ./git-spice-<version>-r1.<arch>.apk
    ```

### Snap

<!-- gs:version v0.27.0 -->

If you use Snap, install the published `git-spice` snap with:

```bash
sudo snap install git-spice --classic
```

The snap uses `classic` confinement
so it can work with your existing Git repositories and system Git binary.

### Manual download

You can manually download the latest release of git-spice from the
[GitHub Releases page](https://github.com/abhinav/git-spice/releases).

## Build from source

To **build from source**, follow these steps:


1. Install the [Go compiler](https://go.dev/dl).
2. Run the following command:

    ```bash
    go install go.abhg.dev/gs@latest
    ```

## Recommended: add a `gs` alias

The canonical command name is `git-spice`,
but most examples in this documentation use `gs` for brevity.
Add a shell alias so both forms work:

=== "Bash / Zsh"

    ```bash
    alias gs=git-spice
    ```

=== "Fish"

    ```fish
    alias gs git-spice
    ```

## Next steps

- [ ] [Create your first stack](stack.md)
