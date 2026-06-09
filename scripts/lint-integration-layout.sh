#!/usr/bin/env bash
# Enforce the repository-level integration-test layout.
#
# Integration suites belong under test/integration/**. Reusable harness code may
# live under internal/testdb/**. Business packages should not grow ad hoc
# *_io_test.go files or package-adjacent integration-tag tests.

set -euo pipefail

cd "$(dirname "$0")/.."

fail=0

while IFS= read -r f; do
  case "$f" in
    test/integration/*|internal/testdb/*) ;;
    *)
      echo "ERROR $f: integration-tagged Go files must live under test/integration/**; helpers may live under internal/testdb/**" >&2
      fail=1
      ;;
  esac
done < <(git ls-files '*.go' | while IFS= read -r f; do
  if grep -qE '^//go:build .*integration' "$f"; then
    printf '%s\n' "$f"
  fi
done)

while IFS= read -r f; do
  echo "ERROR $f: use test/integration/<scope>/<area>/*_test.go instead of package-adjacent *_io_test.go" >&2
  fail=1
done < <(git ls-files '*_io_test.go')

while IFS= read -r d; do
  has_regular_go=0
  while IFS= read -r f; do
    if ! grep -qE '^//go:build .*integration' "$f"; then
      has_regular_go=1
      break
    fi
  done < <(find "$d" -maxdepth 1 -type f -name '*.go' | sort)
  if [ "$has_regular_go" -eq 0 ]; then
    echo "ERROR $d: add an untagged doc.go so go vet ./... can enumerate the package" >&2
    fail=1
  fi
done < <(git ls-files 'test/integration/**/*.go' 'internal/testdb/**/*.go' | while IFS= read -r f; do
  if grep -qE '^//go:build .*integration' "$f"; then
    dirname "$f"
  fi
done | sort -u)

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "lint-integration-layout: clean"
