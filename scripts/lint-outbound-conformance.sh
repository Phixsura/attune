#!/usr/bin/env bash
# lint-outbound-conformance — every outbound adapter must call the shared
# conformance suite and fake-provider harness so redaction,
# response-classification, and delivery-shape checks cannot be skipped by new
# channels.

set -euo pipefail

cd "$(dirname "$0")/.."

status=0

while IFS= read -r dir; do
  if ! find "$dir" -maxdepth 1 -name '*.go' ! -name '*_test.go' | grep -q .; then
    continue
  fi

  conf="$dir/conformance_test.go"
  if [[ ! -f "$conf" ]]; then
    echo "ERROR $dir: missing conformance_test.go" >&2
    status=1
    continue
  fi

  if ! grep -q 'internal/outbound/outboundtest' "$conf"; then
    echo "ERROR $conf: must import internal/outbound/outboundtest" >&2
    status=1
  fi

  if ! grep -Eq 'outboundtest\.Test(Event|Digest|Notification)Channel' "$conf"; then
    echo "ERROR $conf: must call outboundtest.TestEventChannel, TestDigestChannel, or TestNotificationChannel" >&2
    status=1
  fi

  if ! grep -q 'Golden:' "$conf"; then
    echo "ERROR $conf: must declare golden request snapshots" >&2
    status=1
  fi

  if ! grep -Eq 'ProviderShape:[[:space:]]*outboundtest\.ProviderShape' "$conf"; then
    echo "ERROR $conf: must declare an outboundtest.ProviderShape" >&2
    status=1
  fi

  if ! grep -q 'ResponseCases:' "$conf"; then
    echo "ERROR $conf: must declare shared response-classification cases" >&2
    status=1
  fi

  provider="$dir/provider_mock_test.go"
  if [[ ! -f "$provider" ]]; then
    echo "ERROR $dir: missing provider_mock_test.go" >&2
    status=1
    continue
  fi

  if ! grep -q 'internal/outbound/outboundtest' "$provider"; then
    echo "ERROR $provider: must import internal/outbound/outboundtest" >&2
    status=1
  fi

  if ! grep -q 'outboundtest\.NewProvider' "$provider"; then
    echo "ERROR $provider: must use outboundtest.NewProvider" >&2
    status=1
  fi

  if ! grep -q 'Check:' "$provider"; then
    echo "ERROR $provider: must use ProviderScenario.Check for request assertions" >&2
    status=1
  fi

  if grep -q 'Assert:' "$provider"; then
    echo "ERROR $provider: use ProviderScenario.Check instead of goroutine-local Assert" >&2
    status=1
  fi
done < <(find internal/outbound/adapter -mindepth 1 -maxdepth 1 -type d | sort)

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi

echo "lint-outbound-conformance: clean"
