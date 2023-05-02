# 2. Write a PR stacking tool

Date: 2023-04-25

## Status

Accepted

## Context

Pull Request stacking is necessary to make changes in a GitHub repository
while dependent work is still in review.

There are a couple existing tools that already do this:

- [Graphite](https://graphite.dev/)
- [spr](https://github.com/getcord/spr)
- another [spr](https://github.com/ejoffe/spr)

### Why write a new tool?

I did not have success with getting either 'spr' to work.
For one of them, I made an [attempt](https://github.com/ejoffe/spr/pull/280) to
fix the issue I was encountering, and moved on after a month of inactivity.

Graphite matches GitHub's model:
it's branch based, so each changeset gets its own branch.
This doesn't match the workflow I'm aiming for.

## Decision

git-stack is intended to be a tool to create stacked GitHub Pull Requests from
a series of commits.

- Each commit will get a branch and a Pull Request created for it automatically.
- Commits will be the authoritative source of information:
  title, body, content, and parent will all be synced.

The UX for the tool takes inspiration from a similar internal tool that I used
at Uber.

### Name

git-stack is likely a placeholder name while the tool is private.
The public name may be something else.

## Consequences

- It will be possible to check out a repository, commit directly on main,
  and run a command to have PRs formed from them.
- Changes and amendments to PRs will be made with `git rebase -i`
  and going back and amending commits.
- An existing ecosystem of tools like
  [Stacked Git](https://github.com/stacked-git/stgit/) and
  [git-branchless](https://github.com/arxanas/git-branchless) will integrate
  seamlessly with this workflow with the shared language of Git commits.
