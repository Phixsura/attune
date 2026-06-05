#!/usr/bin/env bash
#
# extract-changelog.sh <version> [changelog]
#
# Prints the body of the "## [<version>]" section of a Keep-a-Changelog file
# (everything up to, but not including, the next "## [" heading), with leading
# and trailing blank lines trimmed. Used by .github/workflows/release.yml to
# turn a tag's CHANGELOG entry into GitHub Release notes.
#
# Exits non-zero if the section is missing or empty — a tagged release MUST
# carry its CHANGELOG entry (CLAUDE.md §2). Run it locally before tagging:
#
#     scripts/extract-changelog.sh 0.2.0
#
set -euo pipefail

ver="${1:?usage: extract-changelog.sh <version> [changelog]  (e.g. 0.2.0)}"
changelog="${2:-CHANGELOG.md}"

# First awk: print the lines under "## [<ver>]" up to the next "## [".
# index(...)==1 is a literal prefix match, so the dots/brackets in the version
# need no regex escaping, and "0.2.0" never matches "0.2.0-rc.1".
# Second awk: drop leading + trailing blank lines.
body="$(
  awk -v v="$ver" '
    index($0, "## [" v "]") == 1 { grab = 1; next }
    grab && /^## \[/             { exit }   # next version heading
    grab && /^\[/               { exit }   # footer link-reference defs ([x]: url)
    grab                        { print }
  ' "$changelog" |
  awk 'NF { if (!s) s = NR; e = NR } { a[NR] = $0 } END { for (i = s; i <= e; i++) print a[i] }'
)"

if [ -z "$body" ]; then
  printf 'error: no CHANGELOG entry for version [%s] in %s\n' "$ver" "$changelog" >&2
  exit 1
fi

printf '%s\n' "$body"
