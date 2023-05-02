# 4. Commit metadata storage

Date: 2023-06-25

## Status

Accepted

## Context

We need to store metadata for each commit with that commit.
The metadata will help us track whether it has an existing Pull Request
and any other git-stack specific information.

It must allow for the commit hash to change as the commit is amended,
rebased, and moved around.

Options include:

- Amend commit messages with unique IDs owned by git-stack.
  This has a major caveat that it changes commit hashes,
  so it will break integration with tools like Stacked Git
  which prefer to control the commit workflow.
  It will also lose intermediate branches if the user has created any.
- Git Notes attached to commits, holding a unique ID owned by git-stack.
  This will not change commit hashes, but if we don't use the default
  notes reference (`refs/notes/commits`),
  these notes will not move automatically on rebase
  without having users configure `core.notesRef`.

## Decision

We will store a git-stack-specific identifier in a `git notes` based note
associated with the commit on the *default notes reference*
(`refs/notes/commits`).

git-stack's own internal storage will be on a separate git reference,
mapped to that commit with that unique identifier.


## Consequences

This will ensure that the note is moved by default when the commit is moved,
and more importantly, it's visible to users when they mess it up or drop it.
