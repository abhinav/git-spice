#!/usr/bin/env bash
set -e

if [[ "$CLAUDE_CODE_REMOTE" != "true" ]]; then
	exit 0 # nothing to do for local
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MISE="${MISE:-"$SCRIPT_DIR/mise"}"

"$MISE" trust
"$MISE" install

if [[ -n "${CLAUDE_ENV_FILE:-}" ]]; then
	"$MISE" env >> "$CLAUDE_ENV_FILE"
fi
