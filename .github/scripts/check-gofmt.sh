#!/usr/bin/env bash
# Fails when Go files are not gofmt-formatted. Checks only files changed
# compared to the base commit (see changed-files.sh); pass --all to check
# every tracked Go file.
#
# TODO(repos-dl82): check --all by default once the repository is formatted.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
here=.github/scripts

if [[ "${1:-}" == "--all" ]]; then
	files=$(git ls-files '*.go')
else
	files=$("$here/changed-files.sh" '*.go')
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
