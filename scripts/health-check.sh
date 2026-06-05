#!/usr/bin/env bash
# attune 日常健康检查 — 一行命令看全栈。
#
# 用法 A · 本地 / 任何能联网的机器：
#     bash attune/scripts/health-check.sh
#   只验对外暴露面（nginx 反代到的所有路径）。
#
# 用法 B · 跑在 prod 上（SSH 进去后）：
#     ssh root@47.107.48.110 'bash -s' < attune/scripts/health-check.sh
#   额外多 2 项 prod-only 检查（/health 直连 + /metrics outbox lag）。
#
# 退出码：0 全绿；非 0 = 红线计数。

set -u

PROD_IP="${PROD_IP:-47.107.48.110}"
SMOKE_KEY="${SMOKE_KEY:-fbk_live_14748fac3753548f32efedfe7fa35b8d}"
SMOKE_TENANT="${SMOKE_TENANT:-wanmuchengchuan-dogfood}"

# Public surface = what nginx exposes.
#   :8088 (casceneai-fe nginx)   /fb/v1/...   → attune :8090 (ingest 路径)
#   :8091 (attune-console nginx) /console/    → SPA dist
#   :8091 (attune-console nginx) /fb/v1/...   → attune :8090 (console API)
# Prod-only surface = attune container loopback only.
#   :8090 /health, /metrics

# 检测模式：能联通 127.0.0.1:8090 ⇒ 在 prod；否则用外部 IP。
if curl -fsS --max-time 1 "http://127.0.0.1:8090/health" >/dev/null 2>&1; then
  MODE="prod"
  HOST_BASE="http://127.0.0.1"
  PROD_BASE="http://127.0.0.1:8090"
else
  MODE="external"
  HOST_BASE="http://${PROD_IP}"
  PROD_BASE=""
fi

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; DIM=$'\033[2m'; RESET=$'\033[0m'
FAILED=0
PASSED=0

echo "==> attune health check · mode=${MODE} · $(date '+%F %T')"
echo

check() {
  local name="$1"; shift
  printf "  %-38s " "${name}"
  local out
  if out="$("$@" 2>&1)"; then
    echo "${GREEN}✓${RESET} ${out}"
    PASSED=$((PASSED + 1))
  else
    echo "${RED}✗${RESET} ${out}"
    FAILED=$((FAILED + 1))
  fi
}

# ── 1. 公开面（无论 prod 还是 external 都跑）─────────────

check "console SPA (:8091/console/)" bash -c "
  code=\$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 ${HOST_BASE}:8091/console/)
  [[ \$code == '200' ]] && echo 'HTTP 200' || { echo \"HTTP \$code\"; exit 1; }
"

check "console /me anon (:8091/fb/v1/console/me)" bash -c "
  code=\$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 ${HOST_BASE}:8091/fb/v1/console/me)
  [[ \$code == '401' ]] && echo 'HTTP 401 (expected)' || { echo \"HTTP \$code\"; exit 1; }
"

check "dev-login (:8091/fb/v1/console/install/dev-login)" bash -c "
  code=\$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 \
    \"${HOST_BASE}:8091/fb/v1/console/install/dev-login?tenant=${SMOKE_TENANT}&name=health-check\")
  [[ \$code == '302' ]] && echo 'HTTP 302' || { echo \"HTTP \$code\"; exit 1; }
"

check "ingest smoke (:8088/fb/v1/feedback/ingest)" bash -c "
  resp=\$(curl -sS --max-time 8 -X POST ${HOST_BASE}:8088/fb/v1/feedback/ingest \
    -H 'X-API-Key: ${SMOKE_KEY}' \
    -H 'Content-Type: application/json' \
    -d '{\"user_id\":\"health-check\",\"content\":\"daily health smoke\",\"source\":\"api\"}')
  id=\$(echo \"\$resp\" | sed -n 's/.*\"id\":\([0-9]*\).*/\1/p')
  [[ -n \"\$id\" ]] && echo \"id=\$id\" || { echo \"unexpected: \$resp\"; exit 1; }
"

# ── 2. prod-only 面（仅 SSH 上 prod 跑）─────────────────

if [[ "$MODE" == "prod" ]]; then
  check "attune /health (loopback)" bash -c "
    code=\$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 ${PROD_BASE}/health)
    [[ \$code == '200' ]] && echo 'HTTP 200' || { echo \"HTTP \$code\"; exit 1; }
  "

  check "outbox lag (< 300s)" bash -c "
    lag=\$(curl -sS --max-time 5 ${PROD_BASE}/metrics | awk '/^attune_outbox_lag_seconds /{print \$2; exit}')
    [[ -z \"\$lag\" ]] && { echo 'metric missing'; exit 1; }
    awk -v l=\"\$lag\" 'BEGIN{ if (l < 300) print \"lag=\"l\"s\"; else { print \"lag=\"l\"s (>300!)\"; exit 1 } }'
  "

  check "container Up" bash -c "
    docker ps --filter name=casceneai-attune --format '{{.Status}}' | head -1 | grep -q '^Up' && echo OK || { echo down; exit 1; }
  "
fi

echo
if [[ $FAILED -eq 0 ]]; then
  echo "${GREEN}全绿 (${PASSED} 项)。attune 是 solid 的。${RESET}"
else
  echo "${RED}${FAILED} 项红线 / ${PASSED} 项过。${RESET}"
  echo "${DIM}  排查：ssh root@${PROD_IP} 'docker logs --tail 50 casceneai-casceneai-attune-1'${RESET}"
fi
exit "$FAILED"
