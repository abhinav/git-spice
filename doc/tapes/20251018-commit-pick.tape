# Demo of 'commit pick' for cherry-picking commits from upstack branches.

Output "20251018-commit-pick.gif"
Hide

# Setup
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "gs repo init && git config spice.experiment.commitPick true" Enter
Type "mkdir unicorn && touch unicorn/{horn,magic} && git add unicorn && gs branch create unicorn -m 'Add unicorns'" Enter
Type "mkdir dragon && touch dragon/{wings,fire} && git add dragon && gs branch create dragon -m 'Deploy dragonfire'" Enter
Type "mkdir phoenix && touch phoenix/{rebirth,flames} && git add phoenix && gs branch create phoenix -m 'Implement phoenix support'" Enter
Type "clear" Enter
Show

Sleep 0.5s

Type "gs ll" Enter Sleep 1s
Type "touch unicorn/sparkle" Enter Sleep 0.5s
Type "git add unicorn/sparkle" Enter Sleep 0.5s
Type "gs cc -m 'Add sparkle ability'" Enter Sleep 1s
Type "gs ll" Enter Sleep 2s

Type "# oops, that belongs to unicorn" Enter Sleep 0.5s
Type "gs branch checkout unicorn" Enter Sleep 2s

Type "gs commit pick" Enter Sleep 2s
Down Sleep 0.5s Down Sleep 0.5s
Up Sleep 0.5s Up Sleep 0.5s
Enter Sleep 2s

Type "gs ll" Enter Sleep 3s
