#!/usr/bin/env bash
set -euo pipefail

image="${ATTUNE_RUNTIME_SMOKE_IMAGE:-attune:runtime-smoke}"
docker_bin="${ATTUNE_RUNTIME_SMOKE_DOCKER:-docker}"

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

exec "${cmd[@]}"
