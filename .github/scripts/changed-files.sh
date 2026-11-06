#!/usr/bin/env bash
# Prints the files (relative to the repository root, one per line) that were
# added, copied, modified or renamed compared to a base commit and still
# exist, optionally limited to git pathspecs (e.g. '*.go' 'frontend/').
#
# Base commit:
#   - $BASE_SHA when set (CI passes the pull request base or the pushed
#     range's "before" commit),
#   - otherwise the merge base of HEAD and origin/main (or main).
# Locally, uncommitted and untracked files count as changed too.
#
# Used by the format checks' --changed mode (quick local runs).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

base="${BASE_SHA:-}"
if [[ -n "$base" && "$base" =~ ^0+$ ]]; then
	base="" # new branch push: no "before" commit
fi
if [[ -n "$base" ]] && ! git cat-file -e "$base^{commit}" 2>/dev/null; then
	echo "changed-files: base $base not found (shallow clone?)" >&2
	exit 1
fi
if [[ -z "$base" ]]; then
	for ref in origin/main main; do
		if git rev-parse -q --verify "$ref^{commit}" >/dev/null; then
			base="$(git merge-base HEAD "$ref")"
			break
		fi
	done
fi
if [[ -z "$base" ]]; then
	echo "changed-files: no base commit (set BASE_SHA or fetch main)" >&2
	exit 1
fi

{
	git diff --name-only --diff-filter=ACMR "$base" -- "$@"
	git ls-files --others --exclude-standard -- "$@"
} | sort -u | while IFS= read -r f; do
	[[ -f "$f" ]] && printf '%s\n' "$f"
done
