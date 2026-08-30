# Repository state tracks remote name

We won't assume that the remote name is always `origin`.
We'll let the user pick one and track it in the repository-level state
alongside the trunk branch name.

The remote name will be optional: if not set,
git-spice can still be used to manage and stack branches locally.
A remote name is only needed for operations that push or pull.

This amends the `repo` file in git-spice state to include:

```diff
 {
   // ...
+  remote: string?, // remote name (if any)
 }
```
