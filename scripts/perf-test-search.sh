#!/usr/bin/env bash
# Search performance and query-plan harness.
#
# Usage:
#   scripts/perf-test-search.sh http
#   DATABASE_URL=postgres://... TENANT_ID=... scripts/perf-test-search.sh explain
#   DATABASE_URL=postgres://... TENANT_ID=... scripts/perf-test-search.sh all
#
# Optional environment:
#   BASE_URL       Console origin, default http://localhost:8080
#   API_KEY        Optional bearer token for HTTP runs
#   COOKIE         Optional Cookie header value for Console session auth
#   REQUESTS       Total HTTP requests, default 100
#   CONCURRENCY    Concurrent HTTP workers, default 10
#   SEARCH_QUERY   Query text, default "checkout invoice billing failure"
#   TENANT_ID      Tenant UUID for EXPLAIN mode
#   LIMIT          Search result limit, default 20

set -euo pipefail

MODE="${1:-http}"
BASE_URL="${BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-}"
COOKIE="${COOKIE:-}"
REQUESTS="${REQUESTS:-100}"
CONCURRENCY="${CONCURRENCY:-10}"
SEARCH_QUERY="${SEARCH_QUERY:-checkout invoice billing failure}"
TENANT_ID="${TENANT_ID:-}"
LIMIT="${LIMIT:-20}"

headers=(-H "Content-Type: application/json")
if [[ -n "$API_KEY" ]]; then
  headers+=(-H "Authorization: Bearer $API_KEY")
fi
if [[ -n "$COOKIE" ]]; then
  headers+=(-H "Cookie: $COOKIE")
fi

payload() {
  printf '{"q":%s,"limit":%s}' "$(json_string "$SEARCH_QUERY")" "$LIMIT"
}

json_string() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  printf '"%s"' "$value"
}

run_http() {
  local endpoint="${BASE_URL}/fb/v1/console/feedback/search"
  local body
  body="$(payload)"

  echo "search perf: HTTP endpoint=$endpoint requests=$REQUESTS concurrency=$CONCURRENCY limit=$LIMIT"
  if command -v hey >/dev/null 2>&1; then
    hey -n "$REQUESTS" -c "$CONCURRENCY" -m POST "${headers[@]}" -d "$body" "$endpoint"
    return
  fi

  echo "search perf: hey not found; using sequential curl smoke timing"
  local start end
  start=$(date +%s)
  for _ in $(seq 1 10); do
    curl -fsS -o /dev/null -X POST "${headers[@]}" -d "$body" "$endpoint"
  done
  end=$(date +%s)
  echo "search perf: 10 sequential requests in $((end - start))s"
}

run_explain() {
  if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "search perf: DATABASE_URL is required for explain mode" >&2
    exit 2
  fi
  if [[ -z "$TENANT_ID" ]]; then
    echo "search perf: TENANT_ID is required for explain mode" >&2
    exit 2
  fi
  if ! command -v psql >/dev/null 2>&1; then
    echo "search perf: psql is required for explain mode" >&2
    exit 2
  fi

  echo "search perf: PostgreSQL lexical EXPLAIN tenant=$TENANT_ID limit=$LIMIT"
  psql "$DATABASE_URL" \
    -v tenant_id="$TENANT_ID" \
    -v q="$SEARCH_QUERY" \
    -v limit="$LIMIT" <<'SQL'
EXPLAIN (ANALYZE, BUFFERS)
WITH input AS (
  SELECT :'q'::text AS q,
         '%' || replace(replace(replace(:'q'::text, '\', '\\'), '%', '\%'), '_', '\_') || '%' AS pattern
)
SELECT id,
       ts_rank_cd(
         to_tsvector(
           'simple'::regconfig,
           COALESCE(content, '') || ' ' ||
           COALESCE(enriched_title, '') || ' ' ||
           COALESCE(enriched_display_title, '') || ' ' ||
           COALESCE(enriched_rationale, '') || ' ' ||
           COALESCE(source, '') || ' ' ||
           COALESCE(type, '') || ' ' ||
           COALESCE(user_id, '') || ' ' ||
           COALESCE(page_url, '')
         ),
         plainto_tsquery('simple'::regconfig, (SELECT q FROM input))
       ) AS lexical_score
FROM user_feedback
WHERE tenant_id = :'tenant_id'
  AND deleted_at IS NULL
  AND (
    to_tsvector(
      'simple'::regconfig,
      COALESCE(content, '') || ' ' ||
      COALESCE(enriched_title, '') || ' ' ||
      COALESCE(enriched_display_title, '') || ' ' ||
      COALESCE(enriched_rationale, '') || ' ' ||
      COALESCE(source, '') || ' ' ||
      COALESCE(type, '') || ' ' ||
      COALESCE(user_id, '') || ' ' ||
      COALESCE(page_url, '')
    ) @@ plainto_tsquery('simple'::regconfig, (SELECT q FROM input))
    OR content ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR enriched_title ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR enriched_display_title ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR enriched_rationale ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR source ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR type ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR user_id ILIKE (SELECT pattern FROM input) ESCAPE '\'
    OR page_url ILIKE (SELECT pattern FROM input) ESCAPE '\'
  )
ORDER BY lexical_score DESC, created_at DESC, id DESC
LIMIT :limit;
SQL
}

case "$MODE" in
  http)
    run_http
    ;;
  explain)
    run_explain
    ;;
  all)
    run_http
    run_explain
    ;;
  *)
    echo "usage: scripts/perf-test-search.sh [http|explain|all]" >&2
    exit 2
    ;;
esac
