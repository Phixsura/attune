#!/usr/bin/env bash
set -euo pipefail

image="${ATTUNE_RUNTIME_SMOKE_IMAGE:-attune:runtime-smoke}"
docker_bin="${ATTUNE_RUNTIME_SMOKE_DOCKER:-docker}"
max_attempts="${ATTUNE_RUNTIME_SMOKE_BUILD_MAX_ATTEMPTS:-3}"
retry_delay_seconds="${ATTUNE_RUNTIME_SMOKE_BUILD_RETRY_DELAY_SECONDS:-2}"

cmd=("$docker_bin" build -t "$image")

append_build_arg() {
  local env_name="$1"
  local arg_name="$2"
  local value="${!env_name:-}"

  if [ -n "$value" ]; then
    cmd+=(--build-arg "${arg_name}=${value}")
  fi
}

append_build_arg ATTUNE_RUNTIME_SMOKE_NODE_IMAGE ATTUNE_NODE_IMAGE
append_build_arg ATTUNE_RUNTIME_SMOKE_GO_IMAGE ATTUNE_GO_IMAGE
append_build_arg ATTUNE_RUNTIME_SMOKE_RUNTIME_IMAGE ATTUNE_RUNTIME_IMAGE

cmd+=(.)

if [ "${ATTUNE_RUNTIME_SMOKE_DRY_RUN:-}" = "1" ]; then
  printf '%s\n' "${cmd[@]}"
  exit 0
fi

if [[ ! "$max_attempts" =~ ^[1-9][0-9]*$ ]]; then
  echo "runtime-smoke-build: ATTUNE_RUNTIME_SMOKE_BUILD_MAX_ATTEMPTS must be a positive integer" >&2
  exit 2
fi
if [[ ! "$retry_delay_seconds" =~ ^[0-9]+$ ]]; then
  echo "runtime-smoke-build: ATTUNE_RUNTIME_SMOKE_BUILD_RETRY_DELAY_SECONDS must be a non-negative integer" >&2
  exit 2
fi

is_transient_registry_failure() {
  local log_file="$1"
  grep -Eqi 'failed to fetch anonymous token|auth\.docker\.io.*(EOF|timeout|TLS)|registry.*(EOF|timeout|TLS)' "$log_file"
}

for ((attempt = 1; attempt <= max_attempts; attempt++)); do
  log_file="$(mktemp)"
  set +e
  "${cmd[@]}" 2>&1 | tee "$log_file"
  statuses=("${PIPESTATUS[@]}")
  set -e
  build_status="${statuses[0]}"
  tee_status="${statuses[1]}"
  if (( build_status == 0 && tee_status == 0 )); then
    rm -f "$log_file"
    exit 0
  fi
  if (( tee_status != 0 )); then
    rm -f "$log_file"
    exit "$tee_status"
  fi
  if (( attempt == max_attempts )) || ! is_transient_registry_failure "$log_file"; then
    rm -f "$log_file"
    exit "$build_status"
  fi
  rm -f "$log_file"
  echo "runtime-smoke-build: transient registry error; retrying build (${attempt}/${max_attempts})" >&2
  sleep "$retry_delay_seconds"
done
