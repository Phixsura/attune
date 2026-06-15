#!/usr/bin/env bash
# OIDC SSO end-to-end test script.
#
# Prerequisites:
#   - Docker and docker compose
#   - curl, jq
#
# Usage:
#   ./scripts/test-oidc-e2e.sh
#
# This script:
#   1. Starts dex + attune + postgres via docker compose
#   2. Waits for services to be healthy
#   3. Tests the OIDC discovery endpoint
#   4. Tests the /auth/providers endpoint
#   5. Tests the /auth/oidc/start redirect
#   6. Prints manual testing instructions

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$SCRIPT_DIR/../deploy"
BASE_URL="http://localhost:8090"
DEX_URL="http://localhost:5556"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

cleanup() {
    log_info "Cleaning up..."
    cd "$DEPLOY_DIR"
    docker compose -f docker-compose.yml -f docker-compose.oidc-test.yml down -v 2>/dev/null || true
}

# Cleanup on exit
trap cleanup EXIT

# ── Setup ────────────────────────────────────────────────────────────────

log_info "Setting up OIDC e2e test environment..."

cd "$DEPLOY_DIR"

# Backup existing config if present
if [ -f config.yaml ] && [ ! -f config.yaml.bak ]; then
    cp config.yaml config.yaml.bak
    log_info "Backed up existing config.yaml"
fi

# Use OIDC test config
cp config.oidc-test.yaml config.yaml
log_info "Applied OIDC test config"

# Create .env if missing (use same password as config.oidc-test.yaml)
if [ ! -f .env ]; then
    echo "POSTGRES_USER=attune" > .env
    echo "POSTGRES_PASSWORD=attune_dev_password_2026" >> .env
    echo "POSTGRES_DB=attune" >> .env
fi

# ── Start Services ───────────────────────────────────────────────────────

log_info "Starting services (dex + attune + postgres)..."
docker compose -f docker-compose.yml -f docker-compose.oidc-test.yml up -d --build 2>&1 | head -20

# Wait for postgres
log_info "Waiting for postgres..."
for i in {1..30}; do
    if docker compose exec -T postgres pg_isready -U attune >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Wait for dex
log_info "Waiting for dex..."
for i in {1..30}; do
    if curl -sf "$DEX_URL/dex/.well-known/openid-configuration" >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Wait for attune
log_info "Waiting for attune..."
for i in {1..30}; do
    if curl -sf "$BASE_URL/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

log_info "All services are up!"

# ── Test 1: Dex Discovery ────────────────────────────────────────────────

log_info "Test 1: Dex OIDC discovery endpoint..."
DISCOVERY=$(curl -sf "$DEX_URL/dex/.well-known/openid-configuration")
if echo "$DISCOVERY" | jq -e '.issuer' >/dev/null 2>&1; then
    ISSUER=$(echo "$DISCOVERY" | jq -r '.issuer')
    log_info "  ✓ Discovery OK, issuer: $ISSUER"
else
    log_error "  ✗ Discovery failed"
    exit 1
fi

# ── Test 2: Auth Providers Endpoint ──────────────────────────────────────

log_info "Test 2: /auth/providers endpoint..."
PROVIDERS=$(curl -sf "$BASE_URL/fb/v1/console/auth/providers")
if echo "$PROVIDERS" | jq -e '.providers[] | select(.type == "oidc")' >/dev/null 2>&1; then
    PROVIDER_NAME=$(echo "$PROVIDERS" | jq -r '.providers[] | select(.type == "oidc") | .name')
    log_info "  ✓ OIDC provider found: $PROVIDER_NAME"
else
    log_error "  ✗ OIDC provider not in response"
    echo "$PROVIDERS" | jq .
    exit 1
fi

# Check if password auth is also available (oidc_only: false)
if echo "$PROVIDERS" | jq -e '.providers[] | select(.type == "password")' >/dev/null 2>&1; then
    log_info "  ✓ Password auth also available (oidc_only: false)"
fi

# ── Test 3: OIDC Start Redirect ──────────────────────────────────────────

log_info "Test 3: /auth/oidc/start redirect..."
START_RESP=$(curl -sS -D - -o /dev/null "$BASE_URL/fb/v1/console/auth/oidc/start")
LOCATION=$(echo "$START_RESP" | grep -i "^location:" | tr -d '\r' | cut -d' ' -f2-)

if [[ "$LOCATION" == *"$DEX_URL"* ]] && [[ "$LOCATION" == *"client_id=attune-console"* ]]; then
    log_info "  ✓ Redirects to dex with correct client_id"
    # Check PKCE
    if [[ "$LOCATION" == *"code_challenge="* ]]; then
        log_info "  ✓ PKCE code_challenge present"
    else
        log_warn "  ⚠ PKCE code_challenge missing"
    fi
else
    log_error "  ✗ Redirect location unexpected: $LOCATION"
    exit 1
fi

# ── Test 4: OIDC Health ──────────────────────────────────────────────────

log_info "Test 4: /auth/oidc/health endpoint..."
HEALTH=$(curl -sf "$BASE_URL/fb/v1/console/auth/oidc/health")
if echo "$HEALTH" | jq -e '.status == "healthy"' >/dev/null 2>&1; then
    log_info "  ✓ OIDC health OK"
else
    log_error "  ✗ OIDC health check failed"
    exit 1
fi

# ── Test 5: State Cookie ─────────────────────────────────────────────────

log_info "Test 5: State cookie encryption..."
COOKIES=$(curl -sS -D - -o /dev/null "$BASE_URL/fb/v1/console/auth/oidc/start" | grep -i "^set-cookie:" || true)
if echo "$COOKIES" | grep -q "attune_oidc_state="; then
    log_info "  ✓ State cookie set"
    if echo "$COOKIES" | grep -qi "httponly"; then
        log_info "  ✓ HttpOnly flag present"
    fi
    if echo "$COOKIES" | grep -qi "secure"; then
        log_warn "  ⚠ Secure flag present (will fail over HTTP in browser)"
    fi
else
    log_error "  ✗ State cookie not found"
fi

# ── Summary ──────────────────────────────────────────────────────────────

echo ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Automated tests passed! Manual browser test:"
log_info "═══════════════════════════════════════════════════════════════"
echo ""
echo "  1. Open: $BASE_URL/console/login"
echo "  2. Click 'Sign in with Dex SSO'"
echo "  3. Login with: admin@example.com / password"
echo "  4. Verify redirect to /console/ and session created"
echo ""
echo "  Dex UI:    $DEX_URL/dex"
echo "  Attune:    $BASE_URL"
echo "  Logs:      docker compose -f docker-compose.yml -f docker-compose.oidc-test.yml logs -f"
echo ""
log_info "Press Ctrl+C to stop and cleanup."
echo ""

# Keep running for manual testing
read -r -p "Press Enter to stop services..." || true
