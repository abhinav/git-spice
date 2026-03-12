Output "20260312-branch-checkout-scroll.gif"
Set Shell "bash"
Set FontSize 16
Set Width 480
Set Height 260
Set Padding 10
Set CursorBlink false

Hide
Type "cd $(mktemp -d) && git init && git commit --allow-empty -m 'Initial commit'" Enter
Type "alias gs=git-spice" Enter
Type "gs repo init" Enter Sleep 100ms
Type "git add lion && gs branch create lion -m 'Add lion'" Enter Sleep 100ms
Type "touch griffin && git add griffin && gs branch create griffin -m 'Add griffin'" Enter Sleep 100ms
Type "gs branch checkout main" Enter Sleep 100ms
Type "touch squid && git add squid && gs branch create squid -m 'Add squid'" Enter Sleep 100ms
Type "touch kraken && git add kraken && gs branch create kraken -m 'Add kraken'" Enter Sleep 100ms
Type "gs branch checkout main" Enter Sleep 100ms
Type "touch snake && git add snake && gs branch create snake -m 'Add snake'" Enter Sleep 100ms
Type "touch basilisk && git add basilisk && gs branch create basilisk -m 'Add basilisk'" Enter Sleep 100ms
Type "gs branch checkout main" Enter Sleep 100ms
Type "touch eagle && git add eagle && gs branch create eagle -m 'Add eagle'" Enter Sleep 100ms
Type "touch phoenix && git add phoenix && gs branch create phoenix -m 'Add phoenix'" Enter Sleep 100ms
Type "gs branch checkout main" Enter Sleep 100ms
Type "touch scorpion && git add scorpion && gs branch create scorpion -m 'Add scorpion'" Enter Sleep 100ms
Type "touch manticore && git add manticore && gs branch create manticore -m 'Add manticore'" Enter Sleep 100ms
Type "clear" Enter Sleep 100ms
Show

Sleep 500ms

Type "gs ls" Enter
Sleep 2s

Type "gs bco" Enter
Sleep 2s

Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 1s

Down
Sleep 300ms
Down
Sleep 1s

Up
Sleep 1s

Enter
Sleep 2s

Type "gs ls" Enter
Sleep 3s
