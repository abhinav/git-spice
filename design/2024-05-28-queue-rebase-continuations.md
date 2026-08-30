# Rebase continuations need a queue

If a re-entrant operation performs several independent interruptible rebases,
we need to store all the commands to run after the conflicts are resolved.
For example:

```go
func branchOnto(branch string, onto string) {
  aboves := findAboves(branch)
  for _, above := range aboves {
    upstackRestack(above, base(branch))
  }
  otherThings()
}
```

If any of the `upstackRestack` operations are interrupted by a conflict,
they will set up a continuation to re-run themselves afterwards.
However, after that runs, we need to continue with the next `upstackRestack`,
and then the rest of `branchOnto`.

Right now, the continuation is a single *set* operation

```go
func upstackRestack(...) {
  if err := rebase(...); err != nil {
    return rebaseRecover(err, ["upstack", "restack"])
    // continuation = ["upstack", "restack"]
  }
}
```

To support multiple continuations, we need to store them in a queue,
which will get appended to as we move up the stack of dependent commands.

```go
func upstackRestack(...) {
  if err := rebase(...); err != nil {
    return rebaseRecover(err, ["upstack", "restack"])
    // continuation = [["upstack", "restack"]]
  }
}

func branchOnto(...) {
  for ... {
    if err := upstackRestack(...); err != nil {
      return rebaseRecover(err, ["branch", "onto"])
      // continuation = [["upstack", "restack"], ["branch", "onto"]]
    }
  }
}
```

This way, we get a queue of commands to run as conflicts are resolved.

This amends the `rebase-continue` file to store a queue of continuations.

    [
      {
        command: []string, // gs command to run
        branch: string?,   // branch to run the command on
      },
      ...
    ]
