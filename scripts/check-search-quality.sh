#!/usr/bin/env bash
# Verify the committed semantic-search relevance baseline.

set -euo pipefail

cd "$(dirname "$0")/.."

go run ./internal/tools/searchquality "$@"
