# Demo of 'branch split' pre-filling names with the configured prefix.

Output "20260426-branch-split-prefix-prompt.gif"
Set Shell "bash"
Set FontSize 16
Set Width 640
Set Height 420
Set Padding 10
Set CursorBlink false

Hide
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "alias gs=git-spice" Enter
Type "gs repo init" Enter Sleep 100ms
Type "mkdir stardrive && touch stardrive/chimes && git add stardrive && gs branch create stardrive -m 'Tune warp chimes'" Enter Sleep 100ms
Type "touch stardrive/glitter-nebula && git add stardrive/glitter-nebula && gs commit create -m 'Chart glitter nebula'" Enter Sleep 100ms
Type "touch stardrive/comet-tea && git add stardrive/comet-tea && gs commit create -m 'Brew comet tea'" Enter Sleep 100ms
Type "git config spice.branchCreate.prefix 'fleet/'" Enter Sleep 100ms
Type "clear" Enter Sleep 100ms
Show

Sleep 500ms

Type "gs ll" Enter
Sleep 2s

Type "gs branch split" Enter
Sleep 2s

Enter
Sleep 800ms
Enter
Sleep 800ms
Enter
Sleep 1s

Type "warp-chimes"
Sleep 500ms
Enter
Sleep 1s

Type "nebula-chart"
Sleep 500ms
Enter
Sleep 1s

Type "gs ll" Enter
Sleep 3s
