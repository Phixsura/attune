#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

buf_timeout="${BUF_GENERATE_TIMEOUT:-10m}"
sdk_go_paths=(
  --path proto/attune/v1/ingest.proto
  --path proto/attune/v1/audit.proto
  --path proto/attune/v1/gdpr.proto
  --path proto/attune/v1/outbox.proto
  --path proto/attune/v1/mcp_client.proto
  --path proto/attune/v1/tag.proto
  --path proto/attune/v1/workflow.proto
  --path proto/attune/v1/common.proto
)

ensure_local_proto_plugins() {
  local gobin
  gobin="$(go env GOPATH)/bin"
  mkdir -p "$gobin"
  export PATH="$gobin:$PATH"

  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.5
  go install github.com/google/gnostic/cmd/protoc-gen-openapi@v0.7.0
  if ! command -v npx >/dev/null 2>&1; then
    echo "proto fallback requires npx for ts-proto@2.6.1" >&2
    return 1
  fi
}

normalize_ts_proto_imports() {
  perl -0pi -e '
    s/import type \{ ([^;]+) \} from/import { type $1 } from/g;
    s/import \{ type Dimension, I18nString \} from/import { type Dimension, type I18nString } from/g;
    s/import type \{ Dimension, I18nString \} from/import { type Dimension, type I18nString } from/g;
  ' \
    console/src/proto/attune/v1/*.ts \
    console/src/proto/gnostic/openapi/v3/openapiv3.ts \
    console/src/proto/google/api/httpbody.ts \
    sdk/node/src/proto/attune/v1/*.ts \
    sdk/node/src/proto/gnostic/openapi/v3/openapiv3.ts \
    sdk/node/src/proto/google/api/httpbody.ts
}

generate_main() {
  if buf generate --timeout "$buf_timeout"; then
    return 0
  fi
  echo "buf remote codegen unavailable; falling back to local proto plugins" >&2
  ensure_local_proto_plugins
  buf generate --timeout "$buf_timeout" --template buf.gen.local.yaml
  normalize_ts_proto_imports
}

generate_sdk_go() {
  if buf generate --timeout "$buf_timeout" --template buf.gen.sdk-go.yaml "${sdk_go_paths[@]}"; then
    return 0
  fi
  echo "buf remote Go SDK codegen unavailable; falling back to local proto plugins" >&2
  ensure_local_proto_plugins
  buf generate --timeout "$buf_timeout" --template buf.gen.sdk-go.local.yaml "${sdk_go_paths[@]}"
}

case "${1:-all}" in
  all)
    generate_main
    go run ./internal/tools/openapipatch
    generate_sdk_go
    buf lint
    ;;
  main)
    generate_main
    go run ./internal/tools/openapipatch
    buf lint
    ;;
  sdk-go)
    generate_sdk_go
    ;;
  *)
    echo "usage: $0 [all|main|sdk-go]" >&2
    exit 2
    ;;
esac
