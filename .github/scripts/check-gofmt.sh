#!/usr/bin/env bash
# Fails when Go files are not gofmt-formatted. Checks every tracked Go file;
# pass --changed to check only files changed compared to the base commit
# (see changed-files.sh).
#
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
here=.github/scripts

if [[ "${1:-}" == "--changed" ]]; then
	files=$("$here/changed-files.sh" '*.go')
else
	files=$(git ls-files '*.go')
fi
[[ -z "$files" ]] && { echo "gofmt: no Go files to check"; exit 0; }

# shellcheck disable=SC2086 # file names in this repo contain no whitespace
unformatted=$(gofmt -l $files)
if [[ -n "$unformatted" ]]; then
	echo "These Go files are not gofmt-formatted (run 'mise run fmt'):"
	printf '%s\n' "$unformatted"
	exit 1
fi
echo "gofmt: $(wc -w <<<"$files" | tr -d ' ') file(s) OK"
