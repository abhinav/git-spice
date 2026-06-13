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

echo "Bootstrapping test users and repositories..."
docker exec -u git "$CONTAINER_NAME" \
    gitea admin user create \
    --username testadmin \
    --password 'testadmin123!' \
    --email admin@test.example \
    --admin \
    --must-change-password=false

docker exec -u git "$CONTAINER_NAME" \
    gitea admin user create \
    --username test-reviewer \
    --password 'reviewer123!' \
    --email reviewer@test.example \
    --must-change-password=false

TOKEN=$(curl -sf \
    -u testadmin:testadmin123! \
    -H "Content-Type: application/json" \
    -d '{"name":"gs-record","scopes":["write:repository","write:issue","write:user","write:admin","read:user"]}' \
    http://127.0.0.1:3000/api/v1/users/testadmin/tokens \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['sha1'])")

curl -sf \
    -H "Authorization: token $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"test-repo","auto_init":true,"default_branch":"main"}' \
    http://127.0.0.1:3000/api/v1/user/repos >/dev/null

echo "Recording fixtures..."
GITEA_URL=http://127.0.0.1:3000 \
GITEA_TOKEN="$TOKEN" \
GITEA_TEST_OWNER=testadmin \
GITEA_TEST_REPO=test-repo \
GITEA_TEST_FORK_OWNER=test-reviewer \
GITEA_TEST_FORK_REPO=test-fork-repo \
GITEA_TEST_REVIEWER=test-reviewer \
GITEA_TEST_ASSIGNEE=test-reviewer \
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
