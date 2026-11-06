#!/usr/bin/env bash
# Fails when frontend files are not prettier-formatted. Checks only files
# changed compared to the base commit (see changed-files.sh); pass --all to
# check the whole frontend. Needs frontend dependencies (pnpm install).
#
# TODO(repos-dl82): check --all by default once the repository is formatted.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
here=.github/scripts

if [[ "${1:-}" == "--all" ]]; then
	cd frontend && exec pnpm exec prettier --check .
fi

files=$("$here/changed-files.sh" frontend/ | sed 's|^frontend/||')
[[ -z "$files" ]] && { echo "prettier: no frontend files to check"; exit 0; }

cd frontend
# --ignore-unknown skips files prettier has no parser for; .prettierignore
# still applies to explicitly listed files.
# shellcheck disable=SC2086 # file names in this repo contain no whitespace
pnpm exec prettier --check --ignore-unknown $files
