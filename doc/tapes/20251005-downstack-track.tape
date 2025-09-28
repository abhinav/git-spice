# Demo of 'downstack track'.

Output "20251005-downstack-track.gif"
Hide

# Setup
Env PAGER "cat"
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "gs repo init" Enter Sleep 0.5s
Type "git checkout -b water && touch water.txt && git add water.txt && git commit -m 'Pour water'" Enter Sleep 0.5s
Type "git checkout -b earth && touch earth.txt && git add earth.txt && git commit -m 'Add earth'" Enter Sleep 0.5s
Type "git checkout -b air && touch air.txt && git add air.txt && git commit -m 'Create air'" Enter Sleep 0.5s
Type "git checkout -b fire && touch fire.txt && git add fire.txt && git commit -m 'Light fire'" Enter Sleep 0.5s
Type "clear" Enter
Show

Sleep 0.5s

Type "# We have a manual stack of untracked branches" Enter
Type "git log --branches --graph --format='%d %s'" Enter Sleep 2s
Type "gs ls" Enter Sleep 2s

Type "# Track them all in one go" Enter
Type "gs downstack track fire" Sleep 1s Enter
Sleep 2s Enter # select air
Sleep 2s Enter # select earth
Sleep 2s Enter # select water

Sleep 3s Type "gs ls" Enter Sleep 5s
