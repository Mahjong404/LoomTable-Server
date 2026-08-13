#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd -- "$script_dir/../.." && pwd)
work_dir=$(mktemp -d)
export COMPOSE_PROJECT_NAME="loomtable_acceptance_${RANDOM}_$$"
export LOOMTABLE_POSTGRES_PASSWORD="acceptance-${RANDOM}-$$"
export LOOMTABLE_HOST_ADDR=127.0.0.1
export LOOMTABLE_HOST_PORT=$((32000 + RANDOM % 1000))
archive="$work_dir/acceptance-backup.tar.gz"

cleanup() {
  cd "$repo_dir"
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

wait_ready() {
  local url="http://127.0.0.1:${LOOMTABLE_HOST_PORT}/readyz"
  for _ in $(seq 1 60); do
    if curl --silent --fail "$url" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "Server did not become ready" >&2
  docker compose logs server >&2 || true
  return 1
}

wait_postgres() {
  for _ in $(seq 1 60); do
    if docker compose exec -T postgres pg_isready -U loomtable -d loomtable >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "PostgreSQL did not become ready" >&2
  docker compose logs postgres >&2 || true
  return 1
}

cd "$repo_dir"
docker compose up -d postgres
wait_postgres
docker compose --profile ops run --rm --build migrate -dir /app/migrations
bootstrap_json=$(docker compose --profile ops run --rm admin auth bootstrap --name Acceptance)
token=$(printf '%s\n' "$bootstrap_json" | sed -n 's/.*"secret"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [[ -z "$token" ]]; then
  echo "Bootstrap did not return a Token Secret" >&2
  exit 1
fi

docker compose up -d server
wait_ready
workspace_json=$(curl --silent --show-error --fail \
  -X POST "http://127.0.0.1:${LOOMTABLE_HOST_PORT}/v1/workspaces" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: mut_00000000000000000000000000' \
  --data '{"name":"Acceptance workspace"}')
workspace_id=$(printf '%s\n' "$workspace_json" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
if [[ -z "$workspace_id" ]]; then
  echo "Workspace creation did not return an ID" >&2
  exit 1
fi

"$script_dir/backup.sh" "$archive"
"$script_dir/validate-backup.sh" "$archive"
docker compose down -v --remove-orphans
docker compose up -d postgres
wait_postgres
"$script_dir/restore.sh" "$archive" --confirm
docker compose up -d server
wait_ready

workspace_list=$(curl --silent --show-error --fail \
  "http://127.0.0.1:${LOOMTABLE_HOST_PORT}/v1/workspaces" \
  -H "Authorization: Bearer $token")
if [[ "$workspace_list" != *"$workspace_id"* ]]; then
  echo "Restored Server did not return the acceptance Workspace" >&2
  exit 1
fi
echo "LoomTable Compose backup/restore acceptance passed" >&2
