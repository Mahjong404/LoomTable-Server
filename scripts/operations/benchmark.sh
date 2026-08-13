#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd -- "$script_dir/../.." && pwd)
work_dir=$(mktemp -d)
export COMPOSE_PROJECT_NAME="loomtable_benchmark_${RANDOM}_$$"
export LOOMTABLE_POSTGRES_PASSWORD="benchmark-${RANDOM}-$$"
export LOOMTABLE_HOST_ADDR=127.0.0.1
export LOOMTABLE_HOST_PORT=${LOOMTABLE_HOST_PORT:-31201}

warmups=${LOOMTABLE_BENCH_WARMUPS:-5}
measurements=${LOOMTABLE_BENCH_MEASUREMENTS:-30}
base_url="http://${LOOMTABLE_HOST_ADDR}:${LOOMTABLE_HOST_PORT}"
token=""
table_id=""
primary_field_id=""
location_field_id=""
map_view_id=""

cleanup() {
  cd "$repo_dir"
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

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

wait_server() {
  for _ in $(seq 1 60); do
    if curl --silent --fail "${base_url}/readyz" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "Server did not become ready" >&2
  docker compose logs server >&2 || true
  return 1
}

extract_id() {
  sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -n 1
}

api_json() {
  local method=$1
  local path=$2
  local idempotency_key=${3:-}
  local body=${4:-}
  local args=(--silent --show-error --fail -X "$method" "${base_url}${path}" -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json')
  if [[ -n "$idempotency_key" ]]; then
    args+=(-H "Idempotency-Key: ${idempotency_key}")
  fi
  if [[ -n "$body" ]]; then
    args+=(--data "$body")
  fi
  curl "${args[@]}"
}

record_result() {
  local label=$1
  local url=$2
  local body=${3:-}
  local result_file="${work_dir}/${label}.result"
  local response_file="${work_dir}/${label}.json"
  local times_file="${work_dir}/${label}.times"
  local sizes_file="${work_dir}/${label}.sizes"
  : > "$times_file"
  : > "$sizes_file"

  for _ in $(seq 1 "$warmups"); do
    if [[ -n "$body" ]]; then
      curl --silent --show-error --fail -X POST "$url" \
        -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
        --data "$body" -o /dev/null >/dev/null
    else
      curl --silent --show-error --fail -X POST "$url" \
        -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
        -o /dev/null >/dev/null
    fi
  done

  for _ in $(seq 1 "$measurements"); do
    if [[ -n "$body" ]]; then
      result=$(curl --silent --show-error --fail -X POST "$url" \
        -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
        --data "$body" -o "$response_file" -w '%{http_code}\t%{time_total}\t%{size_download}')
    else
      result=$(curl --silent --show-error --fail -X POST "$url" \
        -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
        -o "$response_file" -w '%{http_code}\t%{time_total}\t%{size_download}')
    fi
    IFS=$'\t' read -r status elapsed bytes <<< "$result"
    if [[ "$status" != "200" ]]; then
      echo "${label} returned HTTP ${status}" >&2
      return 1
    fi
    printf '%s\n' "$elapsed" >> "$times_file"
    printf '%s\n' "$bytes" >> "$sizes_file"
  done

  local p50_seconds p95_seconds p50_bytes p95_bytes
  p50_seconds=$(sort -n "$times_file" | awk -v n="$measurements" 'NR == int(n * 0.50 + 0.9999) { print; exit }')
  p95_seconds=$(sort -n "$times_file" | awk -v n="$measurements" 'NR == int(n * 0.95 + 0.9999) { print; exit }')
  p50_bytes=$(sort -n "$sizes_file" | awk -v n="$measurements" 'NR == int(n * 0.50 + 0.9999) { print; exit }')
  p95_bytes=$(sort -n "$sizes_file" | awk -v n="$measurements" 'NR == int(n * 0.95 + 0.9999) { print; exit }')
  awk -v label="$label" -v p50="$p50_seconds" -v p95="$p95_seconds" \
    -v b50="$p50_bytes" -v b95="$p95_bytes" -v samples="$measurements" \
    'BEGIN { printf "BENCHMARK %s samples=%d p50_ms=%.2f p95_ms=%.2f response_p50_bytes=%d response_p95_bytes=%d\n", label, samples, p50 * 1000, p95 * 1000, b50, b95 }' | tee "$result_file"
}

cd "$repo_dir"
docker compose up -d postgres
wait_postgres
docker compose --profile ops run --rm --build migrate -dir /app/migrations

bootstrap_json=$(docker compose --profile ops run --rm admin auth bootstrap --name Benchmark)
token=$(printf '%s\n' "$bootstrap_json" | sed -n 's/.*"secret"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [[ -z "$token" ]]; then
  echo "Bootstrap did not return a Token Secret" >&2
  exit 1
fi

docker compose up -d server
wait_server

workspace_json=$(api_json POST /v1/workspaces mut_00000000000000000000000001 '{"name":"Benchmark workspace"}')
workspace_id=$(printf '%s\n' "$workspace_json" | extract_id)
base_json=$(api_json POST /v1/bases mut_00000000000000000000000002 "{\"workspaceId\":\"${workspace_id}\",\"name\":\"Benchmark base\"}")
base_id=$(printf '%s\n' "$base_json" | extract_id)
table_json=$(api_json POST /v1/tables mut_00000000000000000000000003 "{\"baseId\":\"${base_id}\",\"name\":\"Benchmark table\"}")
table_id=$(printf '%s\n' "$table_json" | sed -n 's/.*"table":{"id":"\([^"]*\)".*/\1/p')
primary_field_id=$(printf '%s\n' "$table_json" | sed -n 's/.*"primaryField":{"id":"\([^"]*\)".*/\1/p')
if [[ -z "$table_id" || -z "$primary_field_id" ]]; then
  echo "Table creation did not return the expected IDs" >&2
  exit 1
fi

location_json=$(api_json POST "/v1/tables/${table_id}/fields" mut_00000000000000000000000004 \
  '{"name":"Location","type":"location","config":{}}')
location_field_id=$(printf '%s\n' "$location_json" | extract_id)
map_json=$(api_json POST "/v1/tables/${table_id}/views" mut_00000000000000000000000005 \
  "{\"name\":\"Map\",\"type\":\"map\",\"config\":{\"locationFieldId\":\"${location_field_id}\"}}")
map_view_id=$(printf '%s\n' "$map_json" | extract_id)
if [[ -z "$location_field_id" || -z "$map_view_id" ]]; then
  echo "Location Field or Map View creation did not return an ID" >&2
  exit 1
fi

docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U loomtable -d loomtable \
  -v table_id="$table_id" -v primary_field_id="$primary_field_id" -v location_field_id="$location_field_id" <<'SQL'
WITH generated AS (
    SELECT gs,
           ((gs * 37) % 120 - 60)::double precision AS lat,
           ((gs * 73) % 340 - 170)::double precision AS lng,
           format('Record %s', lpad(gs::text, 5, '0')) AS title
    FROM generate_series(1, 20000) AS gs
)
INSERT INTO records (id, table_id, revision, values, query_values, search_text, created_at, updated_at)
SELECT 'rec_' || lpad(gs::text, 26, '0'),
       :'table_id',
       1,
       jsonb_build_object(
           :'primary_field_id', title,
           :'location_field_id', jsonb_build_object('lat', lat, 'lng', lng)
       ),
       jsonb_build_object(
           :'primary_field_id', lower(title),
           :'location_field_id', jsonb_build_object('lat', lat, 'lng', lng)
       ),
       lower(title),
       CURRENT_TIMESTAMP - ((20000 - gs) * INTERVAL '1 millisecond'),
       CURRENT_TIMESTAMP - ((20000 - gs) * INTERVAL '1 millisecond')
FROM generated;
ANALYZE records;
SQL

record_count=$(docker compose exec -T postgres psql -At -U loomtable -d loomtable -c "SELECT count(*) FROM records WHERE table_id = '${table_id}' AND deleted_at IS NULL;")
if [[ "$record_count" != "20000" ]]; then
  echo "Benchmark fixture contains ${record_count} records, expected 20000" >&2
  exit 1
fi

query_body="{\"limit\":100,\"projection\":[\"${primary_field_id}\",\"${location_field_id}\"]}"
filter_sort_body="{\"limit\":100,\"projection\":[\"${primary_field_id}\"],\"filter\":{\"kind\":\"rule\",\"fieldId\":\"${primary_field_id}\",\"operator\":\"contains\",\"value\":\"record\"},\"sort\":[{\"fieldId\":\"${primary_field_id}\",\"direction\":\"desc\"}]}"
map_body='{"viewport":{"boxes":[{"west":-180,"south":-85,"east":180,"north":85}]},"zoom":2,"pixelWidth":1000,"pixelHeight":800}'

echo "LoomTable 20k Query/Map benchmark: records=${record_count} warmups=${warmups} measurements=${measurements}" >&2
record_result query "${base_url}/v1/tables/${table_id}/records/query" "$query_body"
if ! grep -q '"totalCount":20000' "${work_dir}/query.json"; then
  echo "Query benchmark response did not report totalCount=20000" >&2
  exit 1
fi
record_result filter_sort "${base_url}/v1/tables/${table_id}/records/query" "$filter_sort_body"
record_result map_viewport "${base_url}/v1/views/${map_view_id}/map/query" "$map_body"
if ! grep -q '"viewportRenderableRecordCount":20000' "${work_dir}/map_viewport.json"; then
  echo "Map benchmark response did not report 20000 renderable records" >&2
  exit 1
fi
record_result map_summary "${base_url}/v1/views/${map_view_id}/map/summary"
if ! grep -q '"matchedRecordCount":20000' "${work_dir}/map_summary.json"; then
  echo "Map summary benchmark response did not report 20000 matched records" >&2
  exit 1
fi

echo "LoomTable 20k Query/Map benchmark passed" >&2

