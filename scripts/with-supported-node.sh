#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -eq 0 ]]; then
	printf '%s\n' 'with-supported-node: command is required' >&2
	exit 2
fi

is_supported_node_version() {
	local version="${1#v}"
	local major="${version%%.*}"
	case "$major" in
		'' | *[!0-9]*) return 1 ;;
	esac
	(( major == 20 || major == 22 || major >= 24 ))
}

node_version() {
	local node_bin="$1"
	"$node_bin" --version 2>/dev/null || true
}

exec_with_node() {
	local node_bin="$1"
	shift
	local node_dir="${node_bin%/*}"
	export PATH="$node_dir:$PATH"
	exec "$@"
}

fail_unsupported_override() {
	local node_bin="$1"
	local version="$2"
	printf 'with-supported-node: ATTUNE_NODE_BIN points to unsupported Node %s at %s\n' "${version:-unknown}" "$node_bin" >&2
	printf '%s\n' 'Use Node 20, 22, or 24+; CI runs Node 22.' >&2
	exit 1
}

if [[ -n "${ATTUNE_NODE_BIN:-}" ]]; then
	version="$(node_version "$ATTUNE_NODE_BIN")"
	if is_supported_node_version "$version"; then
		exec_with_node "$ATTUNE_NODE_BIN" "$@"
	fi
	fail_unsupported_override "$ATTUNE_NODE_BIN" "$version"
fi

candidate_bins=('')
seen_candidates=':'

add_candidate() {
	local node_bin="$1"
	[[ -x "$node_bin" ]] || return 0
	case "$seen_candidates" in
		*":$node_bin:"*) return 0 ;;
	esac
	seen_candidates="${seen_candidates}${node_bin}:"
	candidate_bins+=("$node_bin")
}

path_node="$(command -v node 2>/dev/null || true)"
if [[ -n "$path_node" ]]; then
	add_candidate "$path_node"
fi

search_dirs=('')
if [[ -n "${ATTUNE_NODE_SEARCH_PATHS+x}" ]]; then
	if [[ -n "$ATTUNE_NODE_SEARCH_PATHS" ]]; then
		IFS=: read -r -a search_dirs <<< "$ATTUNE_NODE_SEARCH_PATHS"
	fi
else
	search_dirs=(
		/opt/homebrew/opt/node@22/bin
		/opt/homebrew/opt/node@20/bin
		/opt/homebrew/opt/node@24/bin
		/usr/local/opt/node@22/bin
		/usr/local/opt/node@20/bin
		/usr/local/opt/node@24/bin
	)
fi

for search_dir in "${search_dirs[@]}"; do
	[[ -n "$search_dir" ]] || continue
	add_candidate "$search_dir/node"
done

if [[ -z "${ATTUNE_NODE_SEARCH_PATHS+x}" ]]; then
	for node_bin in \
		/opt/homebrew/Cellar/node@22/*/bin/node \
		/opt/homebrew/Cellar/node@20/*/bin/node \
		/opt/homebrew/Cellar/node@24/*/bin/node \
		/usr/local/Cellar/node@22/*/bin/node \
		/usr/local/Cellar/node@20/*/bin/node \
		/usr/local/Cellar/node@24/*/bin/node
	do
		add_candidate "$node_bin"
	done
fi

for node_bin in "${candidate_bins[@]}"; do
	[[ -n "$node_bin" ]] || continue
	version="$(node_version "$node_bin")"
	if is_supported_node_version "$version"; then
		exec_with_node "$node_bin" "$@"
	fi
done

printf '%s\n' 'with-supported-node: no supported Node runtime found.' >&2
printf '%s\n' 'Install Node 22, or set ATTUNE_NODE_BIN to a Node 20, 22, or 24+ binary.' >&2
exit 127
