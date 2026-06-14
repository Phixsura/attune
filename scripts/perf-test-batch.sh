#!/usr/bin/env bash
# perf-test-batch.sh — HTTP load testing for batch and search endpoints (#30).
# Requires: hey (https://github.com/rakyll/hey) or wrk
#
# Usage:
#   ./scripts/perf-test-batch.sh                    # defaults
#   BASE_URL=https://api.example.com API_KEY=xxx ./scripts/perf-test-batch.sh
#
# Environment variables:
#   BASE_URL    — base URL of the attune server (default: http://localhost:8080)
#   API_KEY     — bearer token for authentication (default: test-key)
#   REQUESTS    — total requests per test (default: 100)
#   CONCURRENCY — concurrent workers (default: 10)

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-test-key}"
REQUESTS="${REQUESTS:-100}"
CONCURRENCY="${CONCURRENCY:-10}"

# Check for hey or fall back to curl-based testing.
if command -v hey &> /dev/null; then
    USE_HEY=true
else
    echo "Warning: 'hey' not found. Install with: go install github.com/rakyll/hey@latest"
    echo "Falling back to sequential curl timing."
    USE_HEY=false
fi

echo "=============================================="
echo "Batch & Search Performance Test"
echo "=============================================="
echo "Base URL:    $BASE_URL"
echo "Requests:    $REQUESTS"
echo "Concurrency: $CONCURRENCY"
echo "Tool:        $(if $USE_HEY; then echo 'hey'; else echo 'curl (sequential)'; fi)"
echo "=============================================="
echo

# Helper: run load test with hey or measure with curl.
run_test() {
    local name="$1"
    local method="$2"
    local endpoint="$3"
    local body="$4"

    echo "=== $name ==="

    if $USE_HEY; then
        hey -n "$REQUESTS" -c "$CONCURRENCY" \
            -m "$method" \
            -H "Authorization: Bearer $API_KEY" \
            -H "Content-Type: application/json" \
            -d "$body" \
            "${BASE_URL}${endpoint}" 2>&1 | grep -E "(Requests/sec|Latency|Status code distribution)"
    else
        # Fallback: time 10 sequential requests.
        local start end elapsed
        start=$(date +%s.%N)
        for i in {1..10}; do
            curl -s -o /dev/null -w "%{http_code}" \
                -X "$method" \
                -H "Authorization: Bearer $API_KEY" \
                -H "Content-Type: application/json" \
                -d "$body" \
                "${BASE_URL}${endpoint}" || true
        done
        end=$(date +%s.%N)
        elapsed=$(echo "$end - $start" | bc)
        echo "10 sequential requests: ${elapsed}s (avg: $(echo "scale=3; $elapsed/10" | bc)s/req)"
    fi
    echo
}

# Test 1: Keyword search (GET-like, but POST for body).
run_test "Keyword Search (limit=20)" \
    "POST" \
    "/fb/v1/console/feedback/search" \
    '{"q":"user login issue","limit":20}'

# Test 2: Semantic search.
# Note: This requires embeddings to be present. If not, fallback to keyword.
run_test "Semantic Search (limit=20)" \
    "POST" \
    "/fb/v1/console/feedback/search" \
    '{"q":"user experience problem","limit":20,"semantic":true}'

# Test 3: Batch tag operation (add tag to multiple items).
# This uses placeholder IDs - replace with real IDs for actual testing.
run_test "Batch Tag Add (10 items)" \
    "POST" \
    "/fb/v1/console/feedback/batch" \
    '{"feedback_ids":[1,2,3,4,5,6,7,8,9,10],"operation":{"tag":{"add_tag_ids":["00000000-0000-0000-0000-000000000001"]}}}'

# Test 4: Batch workflow transition.
run_test "Batch Workflow Transition (10 items)" \
    "POST" \
    "/fb/v1/console/feedback/batch" \
    '{"feedback_ids":[1,2,3,4,5,6,7,8,9,10],"operation":{"workflow":{"to_state_id":"00000000-0000-0000-0000-000000000002"}}}'

# Test 5: Count by filter.
run_test "Count by Filter (urgent)" \
    "POST" \
    "/fb/v1/console/feedback/count" \
    '{"filter":{"urgent":true}}'

# Test 6: List IDs by filter.
run_test "List IDs by Filter (limit=100)" \
    "POST" \
    "/fb/v1/console/feedback/ids" \
    '{"filter":{},"limit":100}'

# Test 7: Job status poll (if job exists).
run_test "Job Status Poll" \
    "GET" \
    "/fb/v1/console/jobs/00000000-0000-0000-0000-000000000000" \
    '{}'

echo "=============================================="
echo "Performance test complete."
echo
echo "Expected targets (p99):"
echo "  - Sync batch (<=100 items): < 500ms"
echo "  - Async job creation: < 100ms"
echo "  - Job status poll: < 50ms"
echo "  - Keyword search: < 100ms"
echo "  - Semantic search (10k items): < 200ms"
echo "=============================================="
