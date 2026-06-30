---
title: Merging stacks
icon: octicons/git-merge-16
description: >-
  Let git-spice merge your stacked change requests.
---

# Merging stacked change requests

<!-- gs:version v0.30.0 -->

!!! important "Experimental feature"

    Stack merging is experimental.
    You must enable the [`merge` experiment](../cli/experiments.md#merge)
    before using the merge commands.

    ```bash
    git config spice.experiment.merge true
    ```

git-spice can merge submitted Change Requests (CRs) from the command line.
For stacked CRs, git-spice merges them bottom-up:
merging CRs at the bottom of the stack,
restacking and resubmitting their upstack as needed
until all CRs have been merged.

```freeze language="ansi"
--8<-- "captures/merge-progress.txt"
```

!!! question "Why does git-spice need to restack while merging?"

    Many repositories merge CRs with squash-merges.
    These replace commits in the merged branch with new commits on trunk.
    Git does not consider the new commit equivalent to the originals,
    so branches upstack from the merged branch point to a stale history.
    These need to be retargeted to trunk, restacked,
    and updated before they can merge.

    See also [Squash-merges restack the upstack](limits.md#squash-merges-restack-the-upstack)
    to read more about the underlying limitation.

## Merge commands

Use $$gs branch merge$$ to merge a branch stacked on top of trunk.

```pikchr float="right"
scale = 0.8
text "main"
up; line go up 0.05in then go right 0.1in
text "feat1" color red
up; line go up 0.05in then go right 0.1in
text "feat2"
up; line go up 0.05in then go right 0.1in
text "feat3"
```

```freeze language="terminal" center="false"
{green}${reset} gs branch checkout feat1
{green}${reset} gs branch merge
{gray}# merges feat1 into main{reset}
```

To merge a branch and all its downstack branches down to trunk
use $$gs downstack merge$$.

```pikchr float="right"
scale = 0.8
text "main"
up; line go up 0.05in then go right 0.1in
text "feat1" color red
up; line go up 0.05in then go right 0.1in
text "feat2" color red
up; line go up 0.05in then go right 0.1in
text "feat3"
```

```freeze language="terminal" center="false"
{green}${reset} gs branch checkout feat2
{green}${reset} gs downstack merge
{gray}# merges feat1, feat2 into main{reset}
```


To merge a branch, all its downstack branches, and all upstack branches
use $$gs stack merge$$.

```pikchr float="right"
scale = 0.8
text "main"
up; line go up 0.05in then go right 0.1in
text "feat1" color red
up; line go up 0.05in then go right 0.1in
text "feat2" color red
up; line go up 0.05in then go right 0.1in
text "feat3" color red
```

```freeze language="terminal" center="false"
{green}${reset} gs branch checkout feat2
{green}${reset} gs stack merge
{gray}# merges feat1, feat2, feat3, feat4 into main{reset}
```

## When is a CR ready to merge?

Before git-spice requests a merge,
it waits for the forge to report that the CR is ready to merge.

The forge decides what "ready to merge" means for the repository.
Depending on the forge and repository settings,
this can include required CI checks, review approvals, or other requirements.

If a CR is not ready to merge within 30 minutes,
git-spice treats the branch as failed, and skips its upstack branches.
The wait time can be changed with the $$spice.merge.mergeTimeout$$ configuration option.

## Merging multiple stacks

All three merge commands accept `--branch`.
Repeat `--branch` to include multiple branches in the selection.
This has different effects depending on the command.

$$gs branch merge$$ merges only the specified branches.
These must collectively form a path to trunk.

```pikchr float="right"
scale = 0.8

text "main"
up; line go up 0.05in then go right 0.1in
text "feat1" color red
up
F1Up: dot invis

line go up 0.05in then go right 0.1in
text "feat2" color red

move to F1Up
line go up 0.15in then go right 0.1in
text "feat3"
up
line go up 0.05in then go right 0.1in
text "feat4"
```

```freeze language="terminal" center="false"
{green}${reset} gs branch merge {mag}--branch{reset} {red}feat1{reset} {mag}--branch{reset} {red}feat2{reset}
{gray}# merges feat1, feat2 into main{reset}
```

$$gs downstack merge$$ selects the downstack paths of the specified branches.
These are combined and merged into trunk.

```pikchr float="right"
scale = 0.8

text "main"
up; line go up 0.05in then go right 0.1in
text "feat1" color red
up
F1Up: dot invis

line go up 0.05in then go right 0.1in
text "feat2" color red

move to F1Up
line go up 0.15in then go right 0.1in
text "feat3" color red
up
line go up 0.05in then go right 0.1in
text "feat4"
```

```freeze language="terminal" center="false"
{green}${reset} gs downstack merge {mag}--branch{reset} {red}feat2{reset} {mag}--branch{reset} {red}feat3{reset}
{gray}# merges feat1, feat2, feat3 into main{reset}
```

$$gs stack merge$$ selects the upstack and downstack paths of the specified branches,
combining the results and merging them into trunk.

```pikchr float="right"
scale = 0.8

text "main" small
up
Main: dot invis

line go up 0.05in then go right 0.1in
text "feat1" color red
up
line go up 0.05in then go right 0.1in
text "feat2" color red
up
line go up 0.05in then go right 0.1in
text "feat3" color red

move to Main
line go up 0.35in then go right 0.1in
text "feat4" color red
up
line go up 0.05in then go right 0.1in
text "feat5" color red
```

```freeze language="terminal" center="false"
{green}${reset} gs stack merge {mag}--branch{reset} {red}feat2{reset} {mag}--branch{reset} {red}feat4{reset}
{gray}# merges all branches into main{reset}
```

## Custom merge processes

git-spice uses the Forge's merge APIs to merge CRs.
This is not always desirable, as some repositories have custom merge processes.
git-spice supports custom merge workflows
with the $$spice.merge.command$$ configuration option.

For example, a GitHub repository may require
a contributor to comment `/merge` on a PR to request a merge.
To support this, configure git-spice like this:

```freeze language="terminal"
git config \
  {red}spice.merge.command{reset} \
  {mag}'gh pr comment "$GIT_SPICE_GITHUB_PR_NUMBER" --body /merge'{reset}
```

If the script is more complex,
the configuration can be pointed to a script or executable instead.

```freeze language="terminal" float="right"
git config \
  {red}spice.merge.command{reset} \
  {mag}"$HOME/bin/request-merge.sh"{reset}
```

```bash title="$HOME/bin/request-merge.sh"
#!/usr/bin/env bash
set -euo pipefail

NUM="${GIT_SPICE_GITHUB_PR_NUMBER}"
LABEL=ready-to-merge
COMMENT="@myfancybot merge"

gh pr edit "$NUM" --add-label "$LABEL"
gh pr comment "$NUM" --body "$COMMENT"
```

git-spice waits for the CR to be ready to merge
before running the configured command,
and waits for the CR to finish merging before moving upstack.
If the merge workflow is slow,
the $$spice.merge.mergeTimeout$$ configuration option
can be used to increase the wait time.

If the command exits with a non-zero exit code,
the merge is considered to have failed
and the upstack branches are not merged.

See $$spice.merge.command$$ for the full set of environment variables
available to the command.
