#!/usr/bin/env bash
set -euo pipefail

# Never accepts a database URL or an existing container/volume. The PostgreSQL
# data directory is tmpfs on a new private network, with no published ports.
cd "$(dirname "$0")/../.."
project="loomtable-integration-$(date +%s)-${RANDOM}"
postgres="${project}-postgres"
runner="${project}-runner"
network_created=false
image_created=false
cleanup() {
  docker rm -f "$postgres" "$runner" >/dev/null 2>&1 || true
  if $network_created; then docker network rm "$project" >/dev/null; fi
  if $image_created; then docker image rm "$project" >/dev/null; fi
}
trap cleanup EXIT

docker build --target build --tag "$project" .
image_created=true
docker network create --internal "$project" >/dev/null
network_created=true
docker run -d --name "$postgres" --network "$project" \
  --tmpfs /var/lib/postgresql/data \
  -e POSTGRES_DB=loomtable_test -e POSTGRES_USER=loomtable \
  -e POSTGRES_HOST_AUTH_METHOD=trust postgres:16-alpine >/dev/null
for attempt in {1..60}; do
  if docker exec "$postgres" pg_isready -U loomtable -d loomtable_test >/dev/null 2>&1; then break; fi
  if [[ $attempt == 60 ]]; then echo "isolated PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 1
done
echo "Isolated test environment: $project (tmpfs; private network; no host ports)"
docker run --rm --name "$runner" --network "$project" \
  -e "LOOMTABLE_TEST_DATABASE_URL=postgres://loomtable@${postgres}:5432/loomtable_test?sslmode=disable" \
  -e LOOMTABLE_TEST_DATABASE_DISPOSABLE=true \
  "$project" sh -c 'go test -count=1 ./... && go vet ./...'
