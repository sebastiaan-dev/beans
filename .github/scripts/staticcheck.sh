#!/usr/bin/env bash
# Runs staticcheck and fails only on findings that are not listed in
# .github/staticcheck-baseline.txt (pre-existing findings, matched by file,
# message and check, ignoring line numbers so unrelated edits don't break
# the match). Baseline entries that no longer occur are reported so they can
# be removed.
#
#   staticcheck.sh                   check against the baseline
#   staticcheck.sh --update-baseline rewrite the baseline from current findings
#
set -euo pipefail

# Pinned; bump deliberately (new versions add checks).
STATICCHECK_VERSION=v0.8.2

cd "$(git rev-parse --show-toplevel)"
baseline=.github/staticcheck-baseline.txt

tooldir="${XDG_CACHE_HOME:-$HOME/.cache}/beans-tools/staticcheck-$STATICCHECK_VERSION-$(go env GOVERSION)"
if [[ ! -x "$tooldir/staticcheck" ]]; then
	GOBIN="$tooldir" go install "honnef.co/go/tools/cmd/staticcheck@$STATICCHECK_VERSION"
fi

set +e
raw=$("$tooldir/staticcheck" -f text ./... 2>&1)
status=$?
set -e
# Exit status 1 means "findings reported"; anything else is a failure to run.
if [[ $status -ne 0 && $status -ne 1 ]]; then
	printf '%s\n' "$raw"
	echo "staticcheck failed to run (exit status $status)"
	exit "$status"
fi

# file:line:col: message (CHECK)  →  file: message (CHECK)
current=$(printf '%s\n' "$raw" | { grep -v '^$' || true; } | sed -E 's/^([^:]+):[0-9]+:[0-9]+: /\1: /' | LC_ALL=C sort)

if [[ "${1:-}" == "--update-baseline" ]]; then
	{
		grep '^#' "$baseline" 2>/dev/null || true
		printf '%s\n' "$current" | grep -v '^$' || true
	} >"$baseline.tmp"
	mv "$baseline.tmp" "$baseline"
	echo "staticcheck: wrote $(grep -vc '^#' "$baseline" || true) finding(s) to $baseline"
	exit 0
fi

known=$({ grep -v -e '^#' -e '^$' "$baseline" || true; } | LC_ALL=C sort)
new=$(LC_ALL=C comm -13 <(printf '%s\n' "$known") <(printf '%s\n' "$current") | grep -v '^$' || true)
gone=$(LC_ALL=C comm -23 <(printf '%s\n' "$known") <(printf '%s\n' "$current") | grep -v '^$' || true)

if [[ -n "$gone" ]]; then
	echo "staticcheck: these baselined findings no longer occur; remove them from $baseline:"
	printf '  %s\n' "$gone"
fi
if [[ -n "$new" ]]; then
	echo "staticcheck: new findings (fix them; see https://staticcheck.dev/docs/checks):"
	# Print the original lines (with positions) for the new findings.
	while IFS= read -r finding; do
		file=${finding%%: *}
		msg=${finding#*: }
		printf '%s\n' "$raw" | grep -F -- "$file:" | grep -F -- ": $msg" || printf '%s\n' "$finding"
	done <<<"$new"
	exit 1
fi
echo "staticcheck: no new findings ($(grep -c . <<<"$known" || true) baselined)"
