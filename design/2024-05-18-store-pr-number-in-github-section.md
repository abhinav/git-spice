# Branch state tracks PR number in a GitHub section

Instead of tracking the PR number in a top-level field,
we're moving it to a `github` section in the per-branch state.
This leaves room for non-GitHub integrations in the future.

This amends the per-branch files in git-spice state:

```diff
 {
     // ...
-    pr: int?,
+    github: {
+        pr: int?,
+    },
 }
```
