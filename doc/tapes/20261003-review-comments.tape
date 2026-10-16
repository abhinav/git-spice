# Demo of drafting and publishing a review comment.

Output "doc/src/img/review-comments.gif"
Require git
Require gs
Set Shell "bash"
Set Theme "Dark Modern"
Set FontSize 18
Set Width 900
Set Height 520
Set Padding 12
Set Framerate 24
Set TypingSpeed 25ms
Set WaitTimeout 30s

Env PAGER "cat"
Env GIT_CONFIG_GLOBAL "/dev/null"
Env GIT_SPICE_FORGE_KIND "shamhub"

Hide

# The recorder script supplies a local ShamHub repository with one open CR.
Type "export PATH=__GS_BIN_DIR__:$PATH; . __SHAMHUB_ENV_FILE__; cd __REVIEW_DEMO_REPO__" Enter
Type "clear" Enter
Show

Type "gs review comment stardrive.go:6 \\" Enter
Type "  -m 'Return an error when the warp chime is silent.'" Enter
Wait+Screen "Drafted comment 1"
Sleep 1s

Type "gs review list --draft-only" Enter
Wait+Screen "Return an error when the warp chime is silent."
Sleep 3s

Type "gs review publish \\" Enter
Type "  --request-changes \\" Enter
Type "  --body 'Please handle silent chimes before launch.'" Enter
Wait+Screen "Published 1 comment"
Sleep 3s

Hide
Type "exit" Enter
