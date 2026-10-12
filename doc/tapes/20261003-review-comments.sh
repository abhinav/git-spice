#!/usr/bin/env bash

# Record 20261003-review-comments.tape against a disposable local ShamHub.

set -euo pipefail

root=$(git rev-parse --show-toplevel)
for program in git gs mise shamhub shamhub-serve; do
	command -v "$program" >/dev/null
done
gs_bin_dir=$(dirname "$(command -v gs)")

demo=$(mktemp -d)
shamhub-serve \
	--env-file "$demo/shamhub.env" \
	>"$demo/shamhub.log" 2>&1 &
shamhub_pid=$!

cleanup_review_comments_demo() {
	kill "$shamhub_pid" 2>/dev/null || true
	rm -rf "$demo"
}
trap cleanup_review_comments_demo EXIT

while [ ! -s "$demo/shamhub.env" ]; do
	sleep 0.05
done
. "$demo/shamhub.env"
export SHAMHUB_USERNAME=alice
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_SPICE_FORGE_KIND=shamhub

mkdir "$demo/repo"
cd "$demo/repo"
git init -b main
git config user.name Alice
git config user.email alice@example.com

cat >stardrive.go <<'EOF'
package stardrive

func Tune(chime string) string {
	return chime
}
EOF

git add stardrive.go
git commit -m 'Build stardrive'
gs --no-prompt repo init

shamhub new origin alice/example.git
shamhub register alice
git push -u origin main
gs --no-prompt auth login

cat >stardrive.go <<'EOF'
package stardrive

import "strings"

func Tune(chime string) string {
	tuned := strings.TrimSpace(chime)
	return tuned
}
EOF

git add stardrive.go
gs --no-prompt branch create tune-warp-chimes -m 'Tune warp chimes'
gs --no-prompt branch submit \
	--fill \
	--no-draft \
	--no-web \
	--nav-comment=false

cd "$root"
sed \
	-e "s|__GS_BIN_DIR__|$gs_bin_dir|g" \
	-e "s|__SHAMHUB_ENV_FILE__|$demo/shamhub.env|g" \
	-e "s|__REVIEW_DEMO_REPO__|$demo/repo|g" \
	doc/tapes/20261003-review-comments.tape \
	>"$demo/review-comments.tape"
env -u NO_COLOR mise exec -- betamax run "$demo/review-comments.tape"
