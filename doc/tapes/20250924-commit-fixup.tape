# Demo of 'commit fixup' for the 0.18 release notes.
# Not used for the documentation.

Output "20250924-commit-fixup.gif"
Hide

# Setup
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "gs repo init && git config spice.experiment.commitFixup true" Enter
Type "mkdir penguin && touch penguin/{flight,fish} && git add penguin && gs branch create penguin -m 'Invent penguins'" Enter
Type "mkdir iguana && touch iguana/{climb,run} && git add iguana && gs branch create iguana -m 'Invent iguanas'" Enter
Type "touch penguin/swim" Enter
Type "clear" Enter
Show

Sleep 0.5s

Type "ls penguin" Enter Sleep 1s
Type "git status" Enter Sleep 1s
Type "gs ll" Enter Sleep 1s

Type "# penguins can swim but they can't fly" Enter Sleep 1s
Type "git add penguin/swim" Enter Sleep 0.5s
Type "git rm penguin/flight" Enter Sleep 0.5s

Type "gs commit fixup" Enter
Sleep 1s
Down Sleep 0.5s Enter Sleep 1s

Type "gs ll" Enter Sleep 1s
Type "git status" Enter Sleep 1s
Type "ls penguin" Enter Sleep 5s
