#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 OUTPUT.tar.gz" >&2
  exit 2
fi

output=$1
if [[ -e "$output" ]]; then
  echo "refusing to overwrite existing backup: $output" >&2
  exit 1
fi

umask 077
work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT

docker compose exec -T postgres pg_dump -Fc -U loomtable -d loomtable >"$work_dir/database.dump"
docker compose run --rm --no-deps -T --entrypoint tar server \
  -C /var/lib/loomtable/attachments -cf - . >"$work_dir/attachments.tar"

schema_versions=$(docker compose exec -T postgres psql -U loomtable -d loomtable -Atc \
  "SELECT COALESCE(string_agg(version, ',' ORDER BY version), '') FROM schema_migrations")
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
server_version=${LOOMTABLE_SERVER_VERSION:-unknown}
cat >"$work_dir/manifest.json" <<EOF
{
  "format": "loomtable-backup-v1",
  "createdAt": "$created_at",
  "serverVersion": "$server_version",
  "schemaVersions": "$schema_versions",
  "files": ["database.dump", "attachments.tar"]
}
EOF

(
  cd "$work_dir"
  sha256sum database.dump attachments.tar manifest.json >SHA256SUMS
)
tar -czf "$output" -C "$work_dir" database.dump attachments.tar manifest.json SHA256SUMS
chmod 600 "$output" 2>/dev/null || true
echo "backup created: $output" >&2
echo "warning: this unencrypted archive contains Token hashes and business data" >&2
