#!/usr/bin/env bash
# Collect read-only evidence from a running full-stack browser acceptance stack.
#
# This helper does not drive the browser and does not mutate application state.
# Use it after the visible, mouse-driven acceptance path has been completed.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/collect-full-stack-evidence.sh \
    --project COMPOSE_PROJECT \
    --compose-file PATH \
    --base-url URL \
    --output-dir DIR \
    [--app-service attune] \
    [--db-service postgres] \
    [--db-user attune] \
    [--db-name attune] \
    [--provider-service github-mock] \
    [--provider-log-path /mock-state/requests.jsonl] \
    [--since 30m]

The script reads health endpoints, docker compose state, Postgres sync tables,
provider mock logs, and service logs. It never clicks the browser and never
creates application records.
EOF
}

project=""
compose_file=""
base_url=""
output_dir=""
app_service="attune"
db_service="postgres"
db_user="attune"
db_name="attune"
provider_service=""
provider_log_path="/mock-state/requests.jsonl"
since="30m"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      project="${2:-}"
      shift 2
      ;;
    --compose-file)
      compose_file="${2:-}"
      shift 2
      ;;
    --base-url)
      base_url="${2:-}"
      shift 2
      ;;
    --output-dir)
      output_dir="${2:-}"
      shift 2
      ;;
    --app-service)
      app_service="${2:-}"
      shift 2
      ;;
    --db-service)
      db_service="${2:-}"
      shift 2
      ;;
    --db-user)
      db_user="${2:-}"
      shift 2
      ;;
    --db-name)
      db_name="${2:-}"
      shift 2
      ;;
    --provider-service)
      provider_service="${2:-}"
      shift 2
      ;;
    --provider-log-path)
      provider_log_path="${2:-}"
      shift 2
      ;;
    --since)
      since="${2:-}"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "collect-full-stack-evidence: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$project" || -z "$compose_file" || -z "$base_url" || -z "$output_dir" ]]; then
  echo "collect-full-stack-evidence: --project, --compose-file, --base-url, and --output-dir are required" >&2
  usage >&2
  exit 2
fi

if [[ ! -f "$compose_file" ]]; then
  echo "collect-full-stack-evidence: compose file not found: $compose_file" >&2
  exit 2
fi

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "collect-full-stack-evidence: missing required command: $1" >&2
    exit 127
  fi
}

require_command curl
require_command docker
require_command git

base_url="${base_url%/}"
mkdir -p "$output_dir"

compose=(docker compose -p "$project" -f "$compose_file")
capture_failures=0
: >"$output_dir/capture-failures.txt"

capture() {
  local name="$1"
  shift
  local file="$output_dir/$name"
  {
    printf '$'
    printf ' %q' "$@"
    printf '\n\n'
    "$@"
  } >"$file" 2>&1 || {
    local status=$?
    printf '\nexit_status=%s\n' "$status" >>"$file"
    printf '%s exit_status=%s\n' "$name" "$status" >>"$output_dir/capture-failures.txt"
    capture_failures=$((capture_failures + 1))
    return 0
  }
}

capture_psql() {
  local name="$1"
  local sql="$2"
  capture "$name" "${compose[@]}" exec -T "$db_service" \
    psql -U "$db_user" -d "$db_name" -v ON_ERROR_STOP=1 -P pager=off -c "$sql"
}

{
  echo "generated_at_utc=$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "cwd=$(pwd)"
  echo "git_head=$(git rev-parse HEAD 2>/dev/null || true)"
  echo "git_branch=$(git branch --show-current 2>/dev/null || true)"
  echo "project=$project"
  echo "compose_file=$compose_file"
  echo "base_url=$base_url"
  echo "app_service=$app_service"
  echo "db_service=$db_service"
  echo "db_name=$db_name"
  echo "provider_service=$provider_service"
  echo "since=$since"
} >"$output_dir/manifest.env"

git status --short >"$output_dir/git-status.txt" 2>&1 || true

capture "compose-ps.txt" "${compose[@]}" ps
capture "healthz.txt" curl -fsS "$base_url/healthz"
capture "readyz.txt" curl -fsS "$base_url/readyz"
capture "startupz.txt" curl -fsS "$base_url/startupz"
capture "console-headers.txt" curl -fsSI "$base_url/console/"

capture_psql "db-schema-version.txt" \
  "SELECT max(version) AS schema_version FROM schema_migrations_feedback;"
capture_psql "db-external-connections-and-mappings.txt" \
  "SELECT c.name, c.provider, c.base_url, c.last_test_status, c.last_tested_at IS NOT NULL AS tested, m.local_object_type, m.external_object_type, m.direction, m.mapping_version, m.enabled FROM external_connections c LEFT JOIN external_object_mappings m ON m.connection_id = c.id ORDER BY c.name, m.local_object_type, m.external_object_type;"
capture_psql "db-external-sync-runs.txt" \
  "SELECT r.id, c.name AS connection, r.direction, r.trigger, r.status, r.attempts, r.records_seen, r.records_changed, r.records_failed, r.conflicts_created, r.error_kind, left(r.error_message, 240) AS error_message, r.input_metadata, r.created_at, r.finished_at FROM external_sync_runs r JOIN external_connections c ON c.id = r.connection_id ORDER BY r.created_at DESC LIMIT 20;"
capture_psql "db-customer-request-issue-links.txt" \
  "SELECT request_id, provider, external_key, external_url, title, status, sync_state, external_status_category, external_assignee, external_updated_at, last_synced_at, sync_error, external_object_link_id FROM customer_request_issue_links ORDER BY updated_at DESC LIMIT 20;"
capture_psql "db-external-object-links.txt" \
  "SELECT local_object_type, local_object_id, external_object_type, external_key, external_url, external_version, sync_state, sync_error, last_synced_at, external_deleted_at, local_deleted_at, tombstone_reason FROM external_object_links ORDER BY updated_at DESC LIMIT 20;"
capture_psql "db-external-object-comments-summary.txt" \
  "SELECT provider, external_object_type, direction, origin, sync_state, count(*) AS rows FROM external_object_comments GROUP BY provider, external_object_type, direction, origin, sync_state ORDER BY provider, external_object_type, direction, origin, sync_state;"

if [[ -n "$provider_service" ]]; then
  capture "provider-requests.txt" "${compose[@]}" exec -T "$provider_service" \
    sh -lc "if [ -f '$provider_log_path' ]; then tail -n 300 '$provider_log_path'; else echo 'provider log not found: $provider_log_path'; fi"
fi

capture "app-logs.txt" "${compose[@]}" logs --since "$since" "$app_service"
grep -Ei 'error|panic|fatal|failed|x509|refused|timeout|externalsync|external.sync|github|customerrequest' \
  "$output_dir/app-logs.txt" >"$output_dir/app-log-findings.txt" || true

cat >"$output_dir/README.md" <<EOF
# Full-Stack Acceptance Evidence

Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

This directory is read-only evidence captured from a running deployment stack.
It does not prove the mouse-driven browser path by itself. Pair it with the
operator screenshots and acceptance notes required by docs/testing.md.

Key files:

- manifest.env
- compose-ps.txt
- healthz.txt / readyz.txt / startupz.txt
- console-headers.txt
- db-external-connections-and-mappings.txt
- db-external-sync-runs.txt
- db-customer-request-issue-links.txt
- db-external-object-links.txt
- db-external-object-comments-summary.txt
- provider-requests.txt, when --provider-service is supplied
- app-log-findings.txt
- capture-failures.txt
EOF

if (( capture_failures > 0 )); then
  echo "collect-full-stack-evidence: wrote $output_dir with $capture_failures failed capture(s)" >&2
  exit 1
fi

echo "collect-full-stack-evidence: wrote $output_dir"
