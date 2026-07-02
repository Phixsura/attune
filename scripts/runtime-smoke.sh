#!/usr/bin/env bash
# Run a production-shape smoke test against a built attune image.
#
# The smoke starts a throwaway pgvector PostgreSQL container, generates a
# throwaway Tink keyset with the image under test, boots attune, and verifies the
# HTTP, Console static asset, metrics, migration, control-tower, and
# classification-quality schema paths that cheap unit tests cannot exercise.

set -euo pipefail

cd "$(dirname "$0")/.."

image="${ATTUNE_RUNTIME_SMOKE_IMAGE:-attune:runtime-smoke}"
postgres_image="${ATTUNE_RUNTIME_SMOKE_POSTGRES_IMAGE:-pgvector/pgvector:pg17}"
host="${ATTUNE_RUNTIME_SMOKE_HOST:-127.0.0.1}"
host_port="${ATTUNE_RUNTIME_SMOKE_PORT:-}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "runtime-smoke: missing required command: $1" >&2
    exit 127
  fi
}

require_command curl
require_command docker

if [[ -z "$host_port" ]]; then
  require_command node
  host_port="$(node scripts/pick-free-port.mjs random)"
fi

smoke_id="attune-smoke-$(date +%s)-$$"
network="${smoke_id}-net"
postgres="${smoke_id}-pg"
app="${smoke_id}-app"
config_dir="$(mktemp -d /tmp/attune-runtime-smoke.XXXXXX)"

cleanup() {
  docker rm -f "$app" "$postgres" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$config_dir"
}
trap cleanup EXIT

echo "runtime-smoke: image=$image postgres=$postgres_image port=$host_port"

docker network create "$network" >/dev/null

docker run -d --name "$postgres" --network "$network" \
  -e POSTGRES_USER=attune \
  -e POSTGRES_DB=attune \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  "$postgres_image" >/dev/null

for attempt in $(seq 1 90); do
  if docker exec "$postgres" pg_isready -U attune -d attune >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if [[ "$attempt" -eq 90 ]]; then
    echo "runtime-smoke: postgres did not become ready" >&2
    docker logs "$postgres" >&2 || true
    exit 1
  fi
done

keyset="$(docker run --rm "$image" secrets generate-keyset)"
cat >"$config_dir/config.yaml" <<EOF
port: 8090

database:
  url: "postgres://attune@$postgres:5432/attune?sslmode=disable"

migrations:
  confirm_lark_delete: true

enricher:
  interval: "30s"
  batch: 10

ingest:
  cors_allowed_origins: []

console:
  base_url: "http://$host:$host_port"
  session_key: "codex-runtime-smoke-session-key-32-plus-chars"
  bootstrap_admin:
    email: "admin@example.com"
    password: "codex-runtime-smoke-password-32-plus-chars"

secrets:
  tink_keyset: |
$(printf '%s\n' "$keyset" | sed 's/^/    /')

observability:
  service_version: "runtime-smoke"
  environment: "test"
  otlp_endpoint: ""
  otlp_traces_path: "/opentelemetry/v1/traces"
  otlp_headers: {}
  otlp_insecure: false

rate_limit:
  per_minute: 60
  burst: 300
  disabled: true

security:
  allow_loopback_egress: true
  allow_private_egress: true
  trusted_proxy_hops: 0

custom_webhooks: []
EOF

docker run -d --name "$app" --network "$network" \
  -p "$host:$host_port:8090" \
  -v "$config_dir/config.yaml:/app/config.yaml:ro" \
  "$image" >/dev/null

for attempt in $(seq 1 120); do
  if curl -fsS "http://$host:$host_port/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if [[ "$attempt" -eq 120 ]]; then
    echo "runtime-smoke: attune did not become ready" >&2
    docker logs "$app" >&2 || true
    exit 1
  fi
done

assert_equals() {
  local label="$1"
  local want="$2"
  local got="$3"
  if [[ "$got" != "$want" ]]; then
    echo "runtime-smoke: ${label}: got ${got}, want ${want}" >&2
    docker logs --tail=80 "$app" >&2 || true
    exit 1
  fi
}

assert_ge() {
  local label="$1"
  local want="$2"
  local got="$3"
  if [[ ! "$got" =~ ^[0-9]+$ ]] || (( got < want )); then
    echo "runtime-smoke: ${label}: got ${got}, want >= ${want}" >&2
    docker logs --tail=80 "$app" >&2 || true
    exit 1
  fi
}

health="$(curl -fsS "http://$host:$host_port/healthz")"
ready="$(curl -fsS "http://$host:$host_port/readyz")"
startup="$(curl -fsS "http://$host:$host_port/startupz")"
assert_equals healthz ok "$health"
assert_equals readyz ok "$ready"
assert_equals startupz ok "$startup"

console_status="$(
  curl -sS -o "$config_dir/console.html" -w '%{http_code}' \
    "http://$host:$host_port/console/analytics/classification-quality"
)"
assert_equals console_classification_quality_http 200 "$console_status"
control_tower_status="$(
  curl -sS -o "$config_dir/control-tower.html" -w '%{http_code}' \
    "http://$host:$host_port/console/control-tower"
)"
assert_equals console_control_tower_http 200 "$control_tower_status"

main_asset_ref="$(grep -o '/console/assets/index-[^" ]*\.js' "$config_dir/console.html" | head -1 || true)"
css_asset_ref="$(grep -o '/console/assets/index-[^" ]*\.css' "$config_dir/console.html" | head -1 || true)"
if [[ -z "$main_asset_ref" || -z "$css_asset_ref" ]]; then
  echo "runtime-smoke: Console HTML did not reference expected JS/CSS assets" >&2
  exit 1
fi

main_asset_status="$(curl -sS -o /dev/null -w '%{http_code}' "http://$host:$host_port$main_asset_ref")"
css_asset_status="$(curl -sS -o /dev/null -w '%{http_code}' "http://$host:$host_port$css_asset_ref")"
assert_equals console_main_asset_http 200 "$main_asset_status"
assert_equals console_css_asset_http 200 "$css_asset_status"

metrics_status="$(curl -sS -o "$config_dir/metrics.txt" -w '%{http_code}' "http://$host:$host_port/metrics")"
metrics_runtime_series="$(grep -Ec '^(go_|attune_)' "$config_dir/metrics.txt" || true)"
assert_equals metrics_http 200 "$metrics_status"
assert_ge metrics_runtime_series 1 "$metrics_runtime_series"

pgvector_version="$(
  docker exec "$postgres" psql -U attune -d attune -tAc \
    "SELECT extversion FROM pg_extension WHERE extname = 'vector'"
)"
migration_version="$(
  docker exec "$postgres" psql -U attune -d attune -tAc \
    "SELECT max(version) FROM schema_migrations_feedback"
)"
quality_tables="$(
  docker exec "$postgres" psql -U attune -d attune -tAc \
    "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'classification_quality%'"
)"
quality_indexes="$(
  docker exec "$postgres" psql -U attune -d attune -tAc \
    "SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname LIKE 'idx_quality%'"
)"
quality_action_tables="$(
  docker exec "$postgres" psql -U attune -d attune -tAc \
    "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='feedback_quality_actions'"
)"

if [[ -z "$pgvector_version" ]]; then
  echo "runtime-smoke: pgvector extension is missing" >&2
  exit 1
fi
assert_ge schema_migrations_feedback_max_version 96 "$migration_version"
assert_ge classification_quality_tables 4 "$quality_tables"
assert_ge classification_quality_indexes 5 "$quality_indexes"
assert_ge feedback_quality_action_tables 1 "$quality_action_tables"

cat <<EOF
runtime-smoke: ok
  healthz=$health
  readyz=$ready
  startupz=$startup
  console_classification_quality_http=$console_status
  console_control_tower_http=$control_tower_status
  console_main_asset_http=$main_asset_status
  console_css_asset_http=$css_asset_status
  metrics_http=$metrics_status
  metrics_runtime_series=$metrics_runtime_series
  pgvector_version=$pgvector_version
  schema_migrations_feedback_max_version=$migration_version
  classification_quality_tables=$quality_tables
  classification_quality_indexes=$quality_indexes
  feedback_quality_action_tables=$quality_action_tables
EOF
