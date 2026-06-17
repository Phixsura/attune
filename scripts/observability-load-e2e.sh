#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8090}"
PROM_URL="${PROM_URL:-http://127.0.0.1:9090}"
GRAFANA_URL="${GRAFANA_URL:-}"
GRAFANA_USER="${GRAFANA_USER:-admin}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin}"
REQUESTS="${REQUESTS:-240}"
CONCURRENCY="${CONCURRENCY:-24}"
SCRAPE_WAIT_SECONDS="${SCRAPE_WAIT_SECONDS:-20}"
WARMUP_WAIT_SECONDS="${WARMUP_WAIT_SECONDS:-20}"
RATE_LIMIT_WARMUP_REQUESTS="${RATE_LIMIT_WARMUP_REQUESTS:-60}"
RATE_LIMIT_REFILL_SECONDS="${RATE_LIMIT_REFILL_SECONDS:-40}"

if [[ -z "${API_KEY:-}" ]]; then
  echo "API_KEY is required" >&2
  exit 2
fi

workdir="$(mktemp -d "${TMPDIR:-/tmp}/attune-observability-load.XXXXXX")"
trap 'rm -rf "$workdir"' EXIT

send_one() {
  local i="$1"
  local body
  if (( i % 11 == 0 )); then
    body='not-json'
  elif (( i % 7 == 0 )); then
    body='{"source":"api","content":"x","sourceUser":"observability-load"}'
  else
    body="{\"source\":\"api\",\"content\":\"observability load sample ${i}: checkout feedback says the issue is reproducible\",\"sourceUser\":\"observability-load\",\"pageUrl\":\"https://example.test/load/${i}\"}"
  fi

  curl -sS -o /dev/null -w '%{http_code}\n' \
    -X POST "$BASE_URL/v1/feedback/ingest" \
    -H "X-API-Key: $API_KEY" \
    -H 'Content-Type: application/json' \
    --data "$body"
}

export BASE_URL API_KEY
export -f send_one

echo "priming metric label sets before load..."
send_one 1 >/dev/null
send_one 11 >/dev/null
if (( RATE_LIMIT_WARMUP_REQUESTS > 0 )); then
  echo "priming rate-limit label set with ${RATE_LIMIT_WARMUP_REQUESTS} burst requests..."
  seq 1 "$RATE_LIMIT_WARMUP_REQUESTS" \
    | xargs -n 1 -P "$CONCURRENCY" bash -c 'send_one "$0" >/dev/null'
fi
echo "waiting ${WARMUP_WAIT_SECONDS}s for baseline scrape..."
sleep "$WARMUP_WAIT_SECONDS"
if (( RATE_LIMIT_WARMUP_REQUESTS > 0 && RATE_LIMIT_REFILL_SECONDS > 0 )); then
  echo "waiting ${RATE_LIMIT_REFILL_SECONDS}s for limiter refill before measured load..."
  sleep "$RATE_LIMIT_REFILL_SECONDS"
fi

seq 1 "$REQUESTS" \
  | xargs -n 1 -P "$CONCURRENCY" bash -c 'send_one "$0"' \
  | sort | uniq -c | tee "$workdir/status-counts.txt"

ok_count="$(awk '$2 == 200 {print $1}' "$workdir/status-counts.txt" | tr -d ' ')"
limited_count="$(awk '$2 == 429 {print $1}' "$workdir/status-counts.txt" | tr -d ' ')"
bad_count="$(awk '$2 == 400 {print $1}' "$workdir/status-counts.txt" | tr -d ' ')"
ok_count="${ok_count:-0}"
limited_count="${limited_count:-0}"
bad_count="${bad_count:-0}"

if (( ok_count < 1 )); then
  echo "expected at least one successful ingest, got $ok_count" >&2
  exit 1
fi
if (( bad_count < 1 )); then
  echo "expected malformed samples to produce 400 responses, got $bad_count" >&2
  exit 1
fi

echo "waiting ${SCRAPE_WAIT_SECONDS}s for scrape windows..."
sleep "$SCRAPE_WAIT_SECONDS"

prom_query() {
  local query="$1"
  curl -sS --get "$PROM_URL/api/v1/query" --data-urlencode "query=$query"
}

assert_nonempty_query() {
  local label="$1"
  local query="$2"
  local resp
  resp="$(prom_query "$query")"
  echo "$resp" > "$workdir/${label}.json"
  if [[ "$(jq -r '.status' "$workdir/${label}.json")" != "success" ]]; then
    echo "Prometheus query failed: $label" >&2
    cat "$workdir/${label}.json" >&2
    exit 1
  fi
  if [[ "$(jq '.data.result | length' "$workdir/${label}.json")" -lt 1 ]]; then
    echo "Prometheus query returned no data: $label" >&2
    cat "$workdir/${label}.json" >&2
    exit 1
  fi
}

assert_nonempty_query ingest_total 'sum by (result) (attune_ingest_total)'
assert_nonempty_query ingest_rate 'sum(rate(attune_ingest_total[5m])) * 60'
assert_nonempty_query ingest_increase 'sum by (result) (increase(attune_ingest_total[1h]))'
assert_nonempty_query rate_limit_total 'sum(attune_ingest_rate_limit_total) or vector(0)'
assert_nonempty_query rate_limit_increase 'sum(increase(attune_ingest_rate_limit_total[1h])) or vector(0)'
assert_nonempty_query triage_total 'sum by (decision) (attune_triage_decisions_total) or vector(0)'
assert_nonempty_query triage_increase 'sum by (decision) (increase(attune_triage_decisions_total[1h])) or vector(0)'
assert_nonempty_query outbox_lag 'attune_outbox_lag_seconds'
assert_nonempty_query recording_ingest_validation_ratio 'attune:ingest_validation_error_ratio:ratio5m or vector(0)'
assert_nonempty_query recording_enrich_p95 'attune:enrich_p95_seconds:5m or vector(0)'

prom_query_rules="$(curl -sS "$PROM_URL/api/v1/rules")"
echo "$prom_query_rules" > "$workdir/prometheus-rules.json"
if [[ "$(jq -r '.status' "$workdir/prometheus-rules.json")" != "success" ]]; then
  echo "Prometheus rules API failed" >&2
  cat "$workdir/prometheus-rules.json" >&2
  exit 1
fi
for group in attune.recording.sli attune.recording.inbound attune.alerts.ingest attune.alerts.inbound attune.alerts.ai attune.alerts.operations attune.alerts.security; do
  if ! jq -e --arg group "$group" '.data.groups[] | select(.name == $group)' "$workdir/prometheus-rules.json" >/dev/null; then
    echo "Prometheus rule group missing: $group" >&2
    cat "$workdir/prometheus-rules.json" >&2
    exit 1
  fi
done

if [[ -n "$GRAFANA_URL" ]]; then
  grafana_query() {
    local query="$1"
    curl -sS -u "$GRAFANA_USER:$GRAFANA_PASSWORD" --get \
      "$GRAFANA_URL/api/datasources/proxy/uid/prometheus/api/v1/query" \
      --data-urlencode "query=$query"
  }
  grafana_query 'sum(rate(attune_ingest_total[5m])) * 60' > "$workdir/grafana-ingest-rate.json"
  if [[ "$(jq -r '.status' "$workdir/grafana-ingest-rate.json")" != "success" ]]; then
    echo "Grafana datasource proxy query failed" >&2
    cat "$workdir/grafana-ingest-rate.json" >&2
    exit 1
  fi
fi

cat <<EOF
observability load e2e passed
requests=$REQUESTS concurrency=$CONCURRENCY ok=$ok_count rate_limited=$limited_count bad_request=$bad_count
prometheus=$PROM_URL grafana=${GRAFANA_URL:-skipped}
EOF
