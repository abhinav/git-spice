# Tracking git-spice state in a local Git ref

<!--
The filename date preserves this decision's position in the original log.
The exact decision date is unknown.
-->

State required by git-spice will be tracked in a local Git ref.
The ref will point to a *commit object*, which tracks a tree
holding state for every tracked Git branch,
and any requisite repository-level information.

Each branch will be stored as a JSON object (probably)
with the following state.

    {
        base: {
          name: string, // base branch name
          hash: string, // base branch tip hash
        },
        pr: int?,     // pull request number
    }

Repository-level state will include at least:

    {
      trunk: string, // main branch name
    }

Possible example layout:

    repo            // repository information
    branches/
      feature1
      user1/feature2
      <branch-name> // branch information

Choices worth highlighting:

- The Git ref for git-spice state points to a commit object, not a tree.
  This will give us a historical operation log over time,
  should that ever become a command worth exposing.
- Branches are tracked as entries inside the same ref
  instead of ref-per-branch (e.g. `refs/gs/branches/$branch`),
  even at the cost of implementation complexity.
  This has the advantage of not polluting .git with excessive noise.
