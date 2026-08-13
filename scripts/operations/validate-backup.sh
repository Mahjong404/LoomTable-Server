#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 BACKUP.tar.gz" >&2
  exit 2
fi

archive=$1
work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
tar -xzf "$archive" -C "$work_dir"

for required in database.dump attachments.tar manifest.json SHA256SUMS; do
  [[ -f "$work_dir/$required" ]] || { echo "backup is missing $required" >&2; exit 1; }
done
grep -q '"format": "loomtable-backup-v1"' "$work_dir/manifest.json" || {
  echo "unsupported backup format" >&2
  exit 1
}
(
  cd "$work_dir"
  sha256sum -c SHA256SUMS
)
echo "backup is valid: $archive" >&2
