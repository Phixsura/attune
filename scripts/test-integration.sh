#!/usr/bin/env bash
# Run the PostgreSQL integration tier against a real pgvector PostgreSQL.
#
# CI sets ATTUNE_TEST_DATABASE_URL to its service container. For local runs, this
# script starts one shared pgvector container and lets internal/testdb create a
# temporary migrated database per test, matching the CI isolation model without
# paying a Docker container startup for every test case.

set -euo pipefail

cd "$(dirname "$0")/.."

postgres_image="${ATTUNE_TEST_POSTGRES_IMAGE:-pgvector/pgvector:pg17}"
postgres_shm_size="${ATTUNE_TEST_POSTGRES_SHM_SIZE:-1g}"
timeout="${ATTUNE_TEST_INTEGRATION_TIMEOUT:-30m}"
packages=("$@")
if [[ "${#packages[@]}" -eq 0 ]]; then
  packages=(./test/integration/postgres/...)
fi

run_tests() {
  go test -tags=integration -count=1 -p 1 -timeout="$timeout" "${packages[@]}"
}

if [[ -n "${ATTUNE_TEST_DATABASE_URL:-}" ]]; then
  run_tests
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "test-integration: docker not found; using internal/testdb local PostgreSQL fallback"
  run_tests
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  echo "test-integration: docker daemon unavailable; using internal/testdb local PostgreSQL fallback"
  run_tests
  exit 0
fi

test_id="attune-integration-$(date +%s)-$$"
postgres="${test_id}-pg"

cleanup() {
  docker rm -f "$postgres" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "test-integration: starting $postgres_image as $postgres"
docker run -d --rm --name "$postgres" \
  --shm-size "$postgres_shm_size" \
  -e POSTGRES_USER=attune \
  -e POSTGRES_PASSWORD=attune \
  -e POSTGRES_DB=attune \
  -p 127.0.0.1::5432 \
  "$postgres_image" >/dev/null

for attempt in $(seq 1 90); do
  if docker exec "$postgres" pg_isready -h 127.0.0.1 -U attune -d attune >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if [[ "$attempt" -eq 90 ]]; then
    echo "test-integration: postgres did not become ready" >&2
    docker logs "$postgres" >&2 || true
    exit 1
  fi
done

docker exec "$postgres" sh -c 'echo "host replication all all scram-sha-256" >> "$PGDATA/pg_hba.conf"'
docker exec -e PGPASSWORD=attune "$postgres" \
  psql -h 127.0.0.1 -U attune -d attune -c 'SELECT pg_reload_conf();' >/dev/null

host_port="$(docker port "$postgres" 5432/tcp | awk -F: 'NR == 1 { print $NF }')"
if [[ -z "$host_port" ]]; then
  echo "test-integration: could not determine mapped PostgreSQL port" >&2
  exit 1
fi

export ATTUNE_TEST_DATABASE_URL="postgres://attune:attune@127.0.0.1:${host_port}/attune?sslmode=disable"
echo "test-integration: postgres=$postgres port=$host_port shm=$postgres_shm_size timeout=$timeout"

run_tests
