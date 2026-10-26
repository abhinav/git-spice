#!/usr/bin/env bash
# record-gitea-fixtures.sh -- record Gitea integration test fixtures.
#
# Run this script to (re-)record the VCR fixtures used by the Gitea forge
# integration tests.
#
# By default, it starts a local Gitea container, bootstraps test users and
# repositories, records the fixtures, then stops the container.
# The Docker bootstrap prints shell assignments for GITEA_URL, GITEA_TOKEN,
# and GITEA_FORK_TOKEN; this script evaluates those assignments so the
# integration tests can authenticate against the fresh container.
#
# To record against an existing Gitea instance instead:
#
# 1. Edit the gitea section in internal/forge/forgetest/testconfig.yaml.
# 2. Ensure the configured repository and fork repository already exist.
# 3. Give the configured reviewer/assignee write access to the repository.
# 4. Set GITEA_TOKEN and GITEA_FORK_TOKEN.
# 5. Run with GITEA_RECORD_MODE=existing.
#
# Requirements:
#   - Docker for GITEA_RECORD_MODE=docker
#   - An existing Gitea instance for GITEA_RECORD_MODE=existing
#
# Usage:
#   bash tools/record-gitea-fixtures.sh
#   GITEA_RECORD_MODE=existing bash tools/record-gitea-fixtures.sh

set -euo pipefail

GITEA_RECORD_MODE="${GITEA_RECORD_MODE:-docker}"
GITEA_IMAGE="${GITEA_IMAGE:-gitea/gitea:1.22}"
CONTAINER_NAME="gitea-fixture-recorder"

cleanup() {
    if [[ "$GITEA_RECORD_MODE" == "docker" ]]; then
        echo "Stopping Gitea container..."
        docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

case "$GITEA_RECORD_MODE" in
docker)
    echo "Starting Gitea container ($GITEA_IMAGE)..."
    docker run -d \
        --name "$CONTAINER_NAME" \
        -p 3000:3000 \
        -e GITEA__security__INSTALL_LOCK=true \
        -e GITEA__database__DB_TYPE=sqlite3 \
        -e GITEA__server__ROOT_URL=http://localhost:3000 \
        "$GITEA_IMAGE" >/dev/null

    echo "Waiting for Gitea to start..."
    until curl -sf http://127.0.0.1:3000/api/v1/version >/dev/null 2>&1; do
        sleep 2
    done

    echo "Bootstrapping owner account..."
    docker exec -u git "$CONTAINER_NAME" \
        gitea admin user create \
        --username test-owner \
        --password 'owner123!' \
        --email owner@test.example \
        --admin \
        --must-change-password=false

    echo "Bootstrapping test users and repositories..."
    bootstrap_env=$(go run ./tools/ci/gitea-bootstrap)
    eval "$bootstrap_env"
    ;;
existing)
    : "${GITEA_TOKEN:?GITEA_TOKEN is required for existing-instance recording}"
    : "${GITEA_FORK_TOKEN:?GITEA_FORK_TOKEN is required for fork fixture recording}"
    echo "Recording against existing Gitea instance from testconfig.yaml..."
    ;;
*)
    echo "Unknown GITEA_RECORD_MODE: $GITEA_RECORD_MODE" >&2
    echo "Expected 'docker' or 'existing'." >&2
    exit 2
    ;;
esac

export GITEA_TOKEN
export GITEA_FORK_TOKEN
if [[ -n "${GITEA_URL:-}" ]]; then
    export GITEA_URL
fi

echo "Recording fixtures..."
GIT_AUTHOR_EMAIL=bot@example.com \
GIT_AUTHOR_NAME="gs-test[bot]" \
GIT_COMMITTER_EMAIL=bot@example.com \
GIT_COMMITTER_NAME="gs-test[bot]" \
go test \
    -timeout 10m \
    -run "^TestIntegration" \
    ./internal/forge/gitea/ \
    -update

echo "Fixtures recorded successfully."
