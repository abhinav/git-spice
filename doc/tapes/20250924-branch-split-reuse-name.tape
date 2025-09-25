# Demo of 'branch split' supporting moving the branch name.

Output "20250924-branch-split-reuse-name.gif"
Hide

# Setup
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "gs repo init && git config spice.experiment.commitFixup true" Enter
Type "mkdir pangolin && touch pangolin/climb && git add pangolin && git comit -m 'Invent pangolins'" Enter
Type "mkdir penguin && touch penguin/{swim,fish} && git add penguin && gs branch create penguin -m 'Invent penguins'" Enter
Type "touch pangolin/roll && git add pangolin/roll && git commit -m 'Refactor pangolin'" Enter
Type "clear" Enter
Show

Sleep 0.5s
Type "git status" Enter Sleep 1s
Type "gs ll" Enter Sleep 1s

Type "gs branch split" Enter Sleep 2s
Enter Sleep 1s Enter Sleep 1s
Type "penguin" Enter Sleep 1s
Type "refac-pangolin" Enter Sleep 1s

Type "gs ll" Enter Sleep 3s
