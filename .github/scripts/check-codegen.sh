#!/usr/bin/env bash
# Fails when generated code (gqlgen: internal/graph, graphql-codegen:
# frontend/src/lib/graphql/generated.ts) is out of date, i.e. when running the
# generators changes the working tree. Works with a dirty working tree: only
# changes made by the generators count. Needs frontend dependencies.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

snapshot() {
	# Content hashes of all modified and untracked files.
	git status --porcelain=v1 --untracked-files=all -z |
		tr '\0' '\n' | cut -c4- | sort |
		while IFS= read -r f; do
			if [[ -f "$f" ]]; then
				printf '%s %s\n' "$(git hash-object -- "$f")" "$f"
			else
				printf 'deleted %s\n' "$f"
			fi
		done
}

before=$(snapshot)
go generate ./...
(cd frontend && pnpm codegen)
after=$(snapshot)

if [[ "$before" != "$after" ]]; then
	echo "Generated code is out of date; run 'mise codegen' and commit the result."
	diff <(echo "$before") <(echo "$after") || true
	git --no-pager diff --stat
	exit 1
fi
echo "codegen: generated code is up to date"
