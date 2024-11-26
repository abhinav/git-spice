---
title: Your first stack
icon: material/baby-carriage
description: >-
  Create your first stack of branches with git-spice.
  Modify and restack them with ease.
prev_page: install.md
next_page: submit.md
---

With git-spice installed, start experimenting with stacking.

## Initialize git-spice

1. Start by creating an new Git repository to play inside.

    ```bash
    mkdir repo
    cd repo
    git init
    git commit --allow-empty -m "Initial commit"
    ```

2. Initialize git-spice in the repository.
   This will set up the internal storage for git-spice in your repository.

    ```bash
    gs repo init
    ```

    !!! info

        This step isn't absolutely required.
        git-spice will initialize itself automatically when needed.

## Track a branch

Next, stack a branch on top of `main`.

1. Create a new branch and make some changes:

    ```bash
    git checkout -b feat1
    echo "Hello, world!" > hello.txt
    git add hello.txt
    git commit -m "Add hello.txt"
    ```

2. Add the branch to git-spice.

    ```bash
    gs branch track
    ```

This results in a single branch stacked on top of `main`.

```pikchr center="false"
linerad = 0.05in
text "main"
up; line go up 0.15in then go right 0.15in
text "feat1"
```

The above operations are frequently done together.
git-spice provides a command to do all of them in one go: `gs branch create`.

## Use gs branch create

Stack another branch on top of `feat1` with $$gs branch create$$.

1. Check out feat1 and prepare another change:

    ```bash
    echo "This project is cool!" > README.md
    git add README.md
    ```

2. Create a new branch, commit the staged changes,
   and add the branch to git-spice.

    ```bash
    gs branch create feat2
    ```

    !!! tip

        Use `gs branch create -a` to automatically stage changes to tracked
        files.
        This behaves similarly to `git commit -a`.

This results in a stack that looks like this:

```pikchr center="false"
linerad = 0.05in
text "main"
up; line go up 0.15in then go right 0.15in
text "feat1"
up; line go up 0.15in then go right 0.15in
text "feat2"
```

## Modify mid-stack

Time to modify a branch in the middle of the stack.

1. Check out `feat1`:

    ```bash
    gs down
    ```

    !!! info

        The $$gs down$$ command moves us "down" the stack,
        as opposed to $$gs up$$ which moves us "up".

2. Make a change to `hello.txt` and commit it:

    ```bash
    echo "How are you?" >> hello.txt
    git add hello.txt
    git commit -m "Add a question to hello.txt"
    ```

3. **Restack** branches that are out of sync with the current branch.

    ```bash
    gs upstack restack
    ```

    !!! tip

        You can use $$gs commit create$$ to combine the commit and restack steps.

4. Go back to `feat2` and verify:

    ```bash
    gs up
    cat hello.txt
    ```

## Summary

**This section covered:**

- [x] $$gs branch create$$ is a shortcut for creating a branch,
      committing to it, and tracking it with git-spice.
- [x] $$gs up$$ and $$gs down$$ provide relative navigation through the stack.
- [x] $$gs upstack restack$$ updates branches
      that are out of sync with the current branch.

## Next steps

- [ ] Use $$gs commit create$$ to combine the commit and restack steps
- [ ] Explore different flags of $$gs branch create$$
- [ ] [Create your first stacked CRs](submit.md)
