# Branch state tracks upstream branch name

It's possible for a branch to be renamed locally after a `gs branch submit`.
In such a case, we should still push to the original remote branch
instead of creating a new remote branch and pull request.
To make this possible, we'll track the upstream branch name
in the per-branch state.

This amends the per-branch files in git-spice state to include:

```diff
 {
   // ...
+  upstream: string?, // upstream branch name
 }
```
