#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 || $2 != "--confirm" ]]; then
  echo "usage: $0 BACKUP.tar.gz --confirm" >&2
  exit 2
fi

archive=$1
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
"$script_dir/validate-backup.sh" "$archive"

if docker compose ps --status running --services | grep -qx server; then
  echo "refusing to restore while the Server service is running" >&2
  exit 1
fi

work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
tar -xzf "$archive" -C "$work_dir"

docker compose exec -T postgres pg_restore --clean --if-exists --no-owner --no-privileges \
  -U loomtable -d loomtable <"$work_dir/database.dump"
docker compose run --rm --no-deps -T --entrypoint sh server \
  -c 'find /var/lib/loomtable/attachments -mindepth 1 -delete'
docker compose run --rm --no-deps -T --entrypoint tar server \
  -C /var/lib/loomtable/attachments -xf - <"$work_dir/attachments.tar"
echo "restore completed; run migrations and readiness checks before reconnecting clients" >&2
