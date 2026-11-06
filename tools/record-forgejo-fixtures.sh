#!/usr/bin/env bash
# record-forgejo-fixtures.sh records Forgejo integration fixtures with Docker.
#
# It starts a pinned local Forgejo container with sqlite3,
# bootstraps deterministic test users and repositories,
# records the Forgejo VCR fixtures,
# and then removes the container.
#
# The deterministic repository topology is stored in
# internal/forge/forgetest/testconfig.yaml under the forgejo key.
# Keep that config aligned with tools/ci/forgejo-bootstrap/main.go.
# The bootstrap command prints only runtime values,
# such as freshly-created API tokens.
#
# To record against an existing Forgejo instance instead:
#
# 1. Edit the forgejo section in internal/forge/forgetest/testconfig.yaml.
# 2. Ensure the configured repository and fork repository already exist.
# 3. Give the configured reviewer/assignee write access to the repository.
# 4. Set FORGEJO_TOKEN and FORGEJO_FORK_TOKEN.
# 5. Run with FORGEJO_RECORD_MODE=existing.
#
# Requirements:
#   - Docker for FORGEJO_RECORD_MODE=docker
#   - An existing Forgejo instance for FORGEJO_RECORD_MODE=existing
#
# Usage:
#   bash tools/record-forgejo-fixtures.sh
#   FORGEJO_RECORD_MODE=existing bash tools/record-forgejo-fixtures.sh

set -euo pipefail

FORGEJO_RECORD_MODE="${FORGEJO_RECORD_MODE:-docker}"
FORGEJO_IMAGE="${FORGEJO_IMAGE:-codeberg.org/forgejo/forgejo:11.0.15}"
CONTAINER_NAME="${FORGEJO_CONTAINER_NAME:-forgejo-fixture-recorder}"
FORGEJO_HTTP_PORT="${FORGEJO_HTTP_PORT:-3000}"

cleanup() {
    if [[ "$FORGEJO_RECORD_MODE" == "docker" ]]; then
        echo "Stopping Forgejo container..."
        docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

case "$FORGEJO_RECORD_MODE" in
docker)
    FORGEJO_URL="http://127.0.0.1:$FORGEJO_HTTP_PORT"

    echo "Starting Forgejo container ($FORGEJO_IMAGE)..."
    docker run -d \
        --name "$CONTAINER_NAME" \
        -p "$FORGEJO_HTTP_PORT:3000" \
        -e FORGEJO__database__DB_TYPE=sqlite3 \
        -e FORGEJO__security__INSTALL_LOCK=true \
        -e FORGEJO__server__ROOT_URL="$FORGEJO_URL/" \
        "$FORGEJO_IMAGE" >/dev/null

    echo "Waiting for Forgejo to start..."
    until curl -sf "$FORGEJO_URL/api/v1/version" >/dev/null 2>&1; do
        sleep 2
    done

    echo "Creating Forgejo owner account..."
    docker exec -u git "$CONTAINER_NAME" \
        forgejo admin user create \
        --username test-owner \
        --password 'owner123!' \
        --email owner@test.example \
        --admin \
        --must-change-password=false

    echo "Bootstrapping test users and repositories..."
    bootstrap_env=$(FORGEJO_URL="$FORGEJO_URL" mise exec -- go run ./tools/ci/forgejo-bootstrap)
    eval "$bootstrap_env"
    ;;
existing)
    : "${FORGEJO_TOKEN:?FORGEJO_TOKEN is required for existing-instance recording}"
    : "${FORGEJO_FORK_TOKEN:?FORGEJO_FORK_TOKEN is required for fork fixture recording}"
    echo "Recording against existing Forgejo instance from testconfig.yaml..."
    ;;
*)
    echo "Unknown FORGEJO_RECORD_MODE: $FORGEJO_RECORD_MODE" >&2
    echo "Expected 'docker' or 'existing'." >&2
    exit 2
    ;;
esac

export FORGEJO_TOKEN
export FORGEJO_FORK_TOKEN
if [[ -n "${FORGEJO_URL:-}" ]]; then
    export FORGEJO_URL
fi

echo "Recording Forgejo fixtures..."
GIT_AUTHOR_EMAIL=bot@example.com \
GIT_AUTHOR_NAME="gs-test[bot]" \
GIT_COMMITTER_EMAIL=bot@example.com \
GIT_COMMITTER_NAME="gs-test[bot]" \
mise exec -- go test \
    -timeout 10m \
    -run '^TestIntegration$' \
    ./internal/forge/forgejo \
    -update

echo "Forgejo fixtures recorded successfully."
