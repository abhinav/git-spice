# git-spice

## Introduction

<div align="center">
  <img src="doc/src/img/logo.png" width="300"/>
</div>

[![CI](https://github.com/abhinav/git-spice/actions/workflows/ci.yml/badge.svg)](https://github.com/abhinav/git-spice/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/abhinav/git-spice/graph/badge.svg?token=FE4S370I4A)](https://codecov.io/gh/abhinav/git-spice)

</div>

git-spice is a tool for stacking Git branches.
It lets you manage and navigate stacks of branches,
conveniently modify and rebase them,
and create GitHub Pull Requests or GitLab Merge Requests from them.

See <https://abhinav.github.io/git-spice/> for more details.

Usage looks roughly like this:

```shell
# Stack a branch on top of the current branch.
$ gs branch create feat1

# Stack another branch on top of feat1.
$ gs branch create feat2

# Submit pull requests for feat1 and feat2.
$ gs stack submit

# Pull latest changes from the remote repository
# and delete merged branches.
$ gs repo sync

# Restack branches on top of the latest changes.
$ gs stack restack
```

Or equivalently, using [CLI shorthands](https://abhinav.github.io/git-spice/cli/shorthand/):

```shell
$ gs bc feat1  # branch create feat1
$ gs bc feat2  # branch create feat2
$ gs ss        # stack submit
$ gs rs        # repo sync
$ gs sr        # stack restack
```

## Features

- Create, edit, and navigate stacks of branches with ease.
- Submit the entire stack or parts of it with a single command.
  Supports GitHub and GitLab.
- Keep using your existing workflow and adopt git-spice incrementally.
- Completely offline operation with no external dependencies
  until you push or pull from a remote repository.
- Easy-to-remember shorthands for most commands.

## Documentation

See <https://abhinav.github.io/git-spice/> for the full documentation.

## Sponsors

<!-- sponsors --><a href="https://github.com/BugenZhao"><img src="https:&#x2F;&#x2F;github.com&#x2F;BugenZhao.png" width="60px" alt="User avatar: Bugen Zhao" /></a><a href="https://github.com/ichoosetoaccept"><img src="https:&#x2F;&#x2F;github.com&#x2F;ichoosetoaccept.png" width="60px" alt="User avatar: Ismar" /></a><a href="https://github.com/"><img src="https:&#x2F;&#x2F;raw.githubusercontent.com&#x2F;JamesIves&#x2F;github-sponsors-readme-action&#x2F;dev&#x2F;.github&#x2F;assets&#x2F;placeholder.png" width="60px" alt="User avatar: Private Sponsor" /></a><a href="https://github.com/"><img src="https:&#x2F;&#x2F;raw.githubusercontent.com&#x2F;JamesIves&#x2F;github-sponsors-readme-action&#x2F;dev&#x2F;.github&#x2F;assets&#x2F;placeholder.png" width="60px" alt="User avatar: Private Sponsor" /></a><a href="https://github.com/"><img src="https:&#x2F;&#x2F;raw.githubusercontent.com&#x2F;JamesIves&#x2F;github-sponsors-readme-action&#x2F;dev&#x2F;.github&#x2F;assets&#x2F;placeholder.png" width="60px" alt="User avatar: Private Sponsor" /></a><!-- sponsors -->

## License

This software is distributed under the GPL-3.0 License:

```
git-spice: Stacked Pull Requests
Copyright (C) 2024 Abhinav Gupta

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
```

See LICENSE for details.
