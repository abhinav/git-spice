# Merged downstack history propagation

For: [#331](https://github.com/abhinav/git-spice/issues/331)

Per-branch state now tracks a new field:

```diff
 {
   // ...
+  mergedDownstack: []any, // merged downstack history
 }
```

Any time a branch is merged,
its CR information is appended to with its `mergedDownstack`
and pushed to every branch stacked directly on top of it.
Roughly:

```
onMerge(branch) -> {
  newHistory := append(branch.mergedDownstack, branch.changeID)
  for above := range branch.Aboves() {
    above.mergedDownstack = newHistory
  }
}
```

For example, given:

```
    ┏━□ feat3 (#3)
  ┏━┻□ feat2 (#2)
┏━┻□ feat1 (#1)
main

# merge feat1

  ┏━□ feat3 (#3) |
┏━┻□ feat2 (#2)  | mergedDownstack = [#1]
main

# merge feat2

┏━□ feat3 (#3)   | mergedDownstack = [#1, #2]
main
```

This information is used when generating branch navigation comments
to show merged CRs in the comment.
