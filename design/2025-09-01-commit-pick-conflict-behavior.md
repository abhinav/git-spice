# gs commit pick conflict behavior

`gs commit pick` attempts to match `git cherry-pick` behavior for handling
conflicts and working tree state:

- **Staged changes**:
  Cherry-pick fails if any files are staged,
  regardless of whether they conflict with the cherry-picked commit
- **Unstaged changes to modified files**:
  Cherry-pick fails if unstaged changes
  exist in files that the cherry-picked commit also modifies
- **Unstaged changes to unrelated files**:
  Cherry-pick succeeds, unstaged changes are preserved
- **Untracked files**:
  Cherry-pick succeeds, untracked files are preserved
- **Conflicts**: Cherry-pick fails with conflict markers,
  user must run `git cherry-pick --continue` after resolution
