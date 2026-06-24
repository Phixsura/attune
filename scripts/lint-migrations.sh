#!/usr/bin/env bash
# lint-migrations.sh — CI guard for migration file quality (#150)
#
# Checks:
#   A. No duplicate numeric prefixes
#   B. no-transaction migrations are single-statement and idempotent
#   C. Files are named NNN_description.sql with 3-digit prefix
#
# Usage: scripts/lint-migrations.sh [--strict]
#   --strict: exit 1 on warnings (default: exit 1 only on errors)

set -euo pipefail

DIR="internal/infra/database/migrations"
STRICT=false
ERRORS=0
WARNINGS=0

[[ "${1:-}" == "--strict" ]] && STRICT=true

# ─── Check A: Duplicate numeric prefixes ───────────────────────────────────

prefixes=$(ls "$DIR"/*.sql 2>/dev/null | xargs -I{} basename {} | cut -d'_' -f1 | sort)
dups=$(echo "$prefixes" | uniq -d || true)

if [[ -n "$dups" ]]; then
    echo "ERROR: Duplicate migration prefixes detected:"
    for p in $dups; do
        echo "  $p:"
        ls "$DIR"/${p}_*.sql 2>/dev/null | xargs -I{} basename {} | sed 's/^/    /'
    done
    ((ERRORS++)) || true
fi

# ─── Check B: no-transaction directive validation ──────────────────────────

for f in "$DIR"/*.sql; do
    name=$(basename "$f")
    first_line=$(head -1 "$f")

    if echo "$first_line" | grep -q "migrate:no-transaction"; then
        # B1: Count semicolons outside comment lines
        semicolons=$(grep -v "^[[:space:]]*--" "$f" | grep -o ";" | wc -l | tr -d ' ')
        if [[ "$semicolons" -gt 1 ]]; then
            echo "ERROR: $name has no-transaction but multiple statements ($semicolons semicolons)"
            ((ERRORS++)) || true
            continue
        fi

        # B2: Must have idempotency guard
        if ! grep -qiE "IF (NOT )?EXISTS|CONCURRENTLY" "$f"; then
            echo "ERROR: $name has no-transaction but no idempotency guard (IF NOT EXISTS / IF EXISTS / CONCURRENTLY)"
            ((ERRORS++)) || true
        fi
    fi
done

# ─── Check C: Naming convention ────────────────────────────────────────────

for f in "$DIR"/*.sql; do
    name=$(basename "$f")
    if ! echo "$name" | grep -qE "^[0-9]{3}_[a-z0-9_]+\.sql$"; then
        echo "WARNING: $name doesn't match NNN_description.sql pattern"
        ((WARNINGS++)) || true
    fi
done

# ─── Summary ───────────────────────────────────────────────────────────────

echo ""
if [[ $ERRORS -gt 0 ]]; then
    echo "Migration lint FAILED: $ERRORS error(s), $WARNINGS warning(s)"
    exit 1
elif [[ $WARNINGS -gt 0 && "$STRICT" == "true" ]]; then
    echo "Migration lint FAILED (strict mode): $WARNINGS warning(s)"
    exit 1
elif [[ $WARNINGS -gt 0 ]]; then
    echo "Migration lint passed with $WARNINGS warning(s)"
else
    echo "Migration lint passed."
fi
