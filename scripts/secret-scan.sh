#!/usr/bin/env bash
set -euo pipefail

script_path="${BASH_SOURCE[0]}"
case "$script_path" in
	*/*) script_dir="$(cd -- "${script_path%/*}" && pwd)" ;;
	*) script_dir="$(pwd)" ;;
esac
repo_root="$(cd -- "$script_dir/.." && pwd)"

trufflehog_image="${TRUFFLEHOG_IMAGE:-ghcr.io/trufflesecurity/trufflehog:3.95.5}"
results_mode="${TRUFFLEHOG_RESULTS:-verified,unknown}"
exclude_paths="$repo_root/.trufflehogignore"
base_ref="${TRUFFLEHOG_BASE_REF:-origin/main}"
git_range_args=()
git_branch_args=()
runner=""
scan_meta_dir=""
scan_checkout_dir=""
scan_staged_dir=""
docker_snapshot_dir=""

prepare_git_range_args() {
	local head_commit=""
	local base_commit=""
	local branch_name=""

	head_commit="$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || true)"
	base_commit="$(git -C "$repo_root" merge-base HEAD "$base_ref" 2>/dev/null || true)"
	branch_name="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"

	if [[ -n "$base_commit" && -n "$head_commit" && "$base_commit" != "$head_commit" ]]; then
		printf 'secret-scan: scanning commits since %s (%s)\n' "$base_ref" "$base_commit"
		git_range_args=(--since-commit "$base_commit")
	else
		printf 'secret-scan: scanning current HEAD only\n'
		git_range_args=(--max-depth=1)
	fi

	if [[ -n "$branch_name" && "$branch_name" != "HEAD" ]]; then
		git_branch_args=(--branch "$branch_name")
	fi
}

checkout_paths() {
	if [[ -n "${TRUFFLEHOG_CHANGED_PATHS_FILE:-}" ]]; then
		cat "$TRUFFLEHOG_CHANGED_PATHS_FILE"
		return
	fi
	(
		cd "$repo_root"
		git ls-files --cached --modified --others --exclude-standard
	) | sort -u
}

staged_paths() {
	if [[ -n "${TRUFFLEHOG_STAGED_PATHS_FILE:-}" ]]; then
		cat "$TRUFFLEHOG_STAGED_PATHS_FILE"
		return
	fi
	(
		cd "$repo_root"
		git diff --cached --name-only --diff-filter=ACMRTUXB --
	) | sort -u
}

ensure_scan_meta_dir() {
	if [[ -z "$scan_meta_dir" ]]; then
		scan_meta_dir="$(mktemp -d "${TMPDIR:-/tmp}/attune-secret-scan.XXXXXX")"
	fi
}

prepare_checkout_snapshot() {
	ensure_scan_meta_dir
	scan_checkout_dir="$scan_meta_dir/checkout"
	mkdir -p "$scan_checkout_dir"
	while IFS= read -r relative_path; do
		if is_safe_relative_path "$relative_path" && [[ -f "$repo_root/$relative_path" ]]; then
			local target_dir="$scan_checkout_dir"
			if [[ "$relative_path" == */* ]]; then
				target_dir="$scan_checkout_dir/${relative_path%/*}"
			fi
			mkdir -p "$target_dir"
			cp "$repo_root/$relative_path" "$scan_checkout_dir/$relative_path"
		fi
	done < <(checkout_paths)
}

prepare_staged_snapshot() {
	ensure_scan_meta_dir
	scan_staged_dir="$scan_meta_dir/staged"
	mkdir -p "$scan_staged_dir"
	while IFS= read -r relative_path; do
		if is_safe_relative_path "$relative_path"; then
			local target_dir="$scan_staged_dir"
			if [[ "$relative_path" == */* ]]; then
				target_dir="$scan_staged_dir/${relative_path%/*}"
			fi
			mkdir -p "$target_dir"
			if [[ -n "${TRUFFLEHOG_STAGED_CONTENT_ROOT:-}" ]]; then
				if [[ -f "$TRUFFLEHOG_STAGED_CONTENT_ROOT/$relative_path" ]]; then
					cp "$TRUFFLEHOG_STAGED_CONTENT_ROOT/$relative_path" "$scan_staged_dir/$relative_path"
				fi
			else
				git -C "$repo_root" show ":$relative_path" > "$scan_staged_dir/$relative_path"
			fi
		fi
	done < <(staged_paths)
}

is_safe_relative_path() {
	local relative_path="$1"
	[[ -n "$relative_path" ]] || return 1
	case "$relative_path" in
		/* | ../* | */../* | */..) return 1 ;;
	esac
	return 0
}

cleanup() {
	if [[ -n "$scan_meta_dir" ]]; then
		rm -rf "$scan_meta_dir"
	fi
}

trap cleanup EXIT

run_local_trufflehog() {
	trufflehog "$@"
}

run_docker_trufflehog() {
	if [[ -n "$docker_snapshot_dir" ]]; then
		docker run --rm \
			-v "$repo_root:/repo:ro" \
			-v "$docker_snapshot_dir:/snapshot:ro" \
			"$trufflehog_image" \
			"$@"
		return
	fi
	docker run --rm \
		-v "$repo_root:/repo:ro" \
		"$trufflehog_image" \
		"$@"
}

run_trufflehog() {
	case "$runner" in
		local) run_local_trufflehog "$@" ;;
		docker) run_docker_trufflehog "$@" ;;
		*) printf 'secret-scan: internal error: runner is not selected\n' >&2; return 2 ;;
	esac
}

run_git_scan() {
	prepare_git_range_args
	if [[ "$runner" == "docker" ]]; then
		run_trufflehog git \
			"--results=$results_mode" \
			--exclude-paths=/repo/.trufflehogignore \
			--no-update \
			--fail \
			"${git_range_args[@]}" \
			"${git_branch_args[@]}" \
			file:///repo
	else
		run_trufflehog git \
			"--results=$results_mode" \
			"--exclude-paths=$exclude_paths" \
			--no-update \
			--fail \
			"${git_range_args[@]}" \
			"${git_branch_args[@]}" \
			"file://$repo_root"
	fi
}

run_filesystem_scan() {
	local label="$1"
	local snapshot_dir="$2"
	if ! find "$snapshot_dir" -type f -print -quit | grep -q .; then
		printf 'secret-scan: no %s files to scan\n' "$label"
		return 0
	fi
	printf 'secret-scan: scanning %s files\n' "$label"
	if [[ "$runner" == "docker" ]]; then
		docker_snapshot_dir="$snapshot_dir"
		run_trufflehog filesystem \
			"--results=$results_mode" \
			--exclude-paths=/repo/.trufflehogignore \
			--no-update \
			--fail \
			/snapshot
		docker_snapshot_dir=""
	else
		run_trufflehog filesystem \
			"--results=$results_mode" \
			"--exclude-paths=$exclude_paths" \
			--no-update \
			--fail \
			"$snapshot_dir"
	fi
}

run_checkout_scan() {
	prepare_checkout_snapshot
	run_filesystem_scan "current checkout" "$scan_checkout_dir"
}

run_staged_scan() {
	prepare_staged_snapshot
	run_filesystem_scan "staged index" "$scan_staged_dir"
}

if command -v trufflehog >/dev/null 2>&1; then
	printf 'secret-scan: using trufflehog from PATH\n'
	runner="local"
	run_git_scan
	run_checkout_scan
	run_staged_scan
	exit 0
fi

if command -v docker >/dev/null 2>&1; then
	printf 'secret-scan: trufflehog not on PATH; using Docker image %s\n' "$trufflehog_image"
	runner="docker"
	run_git_scan
	run_checkout_scan
	run_staged_scan
	exit 0
fi

printf '%s\n' \
	'secret-scan: TruffleHog is required for local CI preflight.' \
	'Install TruffleHog or start Docker, then rerun:' \
	'  brew install trufflehog' \
	"  docker pull $trufflehog_image" >&2
exit 127
