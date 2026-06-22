#!/usr/bin/env bash
# record-gitea-fixtures.sh -- record Gitea integration test fixtures using Docker.
#
# Run this script to (re-)record the VCR fixtures used by the Gitea forge
# integration tests. It starts a local Gitea container, bootstraps test
# users and repositories, records the fixtures, then stops the container.
#
# Requirements: Docker
#
# Usage:
#   bash tools/record-gitea-fixtures.sh

set -euo pipefail

GITEA_IMAGE="${GITEA_IMAGE:-gitea/gitea:1.22}"
CONTAINER_NAME="gitea-fixture-recorder"

cleanup() {
    echo "Stopping Gitea container..."
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

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
bootstrap_env=$(mise exec -- go run ./tools/ci/gitea-bootstrap)
eval "$bootstrap_env"

echo "Recording fixtures..."
GITEA_URL="$GITEA_URL" \
GITEA_TOKEN="$GITEA_TOKEN" \
GITEA_FORK_TOKEN="$GITEA_FORK_TOKEN" \
GITEA_TEST_OWNER=test-owner \
GITEA_TEST_REPO=test-repo \
GITEA_TEST_FORK_OWNER=test-reviewer \
GITEA_TEST_FORK_REPO=test-fork-repo \
GITEA_TEST_REVIEWER=test-reviewer \
GITEA_TEST_ASSIGNEE=test-reviewer \
GIT_AUTHOR_EMAIL=bot@example.com \
GIT_AUTHOR_NAME="gs-test[bot]" \
GIT_COMMITTER_EMAIL=bot@example.com \
GIT_COMMITTER_NAME="gs-test[bot]" \
mise exec -- go test \
    -timeout 10m \
    -run "^TestIntegration" \
    ./internal/forge/gitea/ \
    -update

echo "Fixtures recorded successfully."
