package graph

import (
	"fmt"
	"strings"
)

// shellQuote quotes s for use as a single POSIX shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// integratePrepareMainScript returns a bash script that the integrate flow
// runs in the worktree right before fast-forwarding the main repo to the
// squashed commit ($SQUASH_SHA, defaulting to the worktree's HEAD).
//
// `git merge --ff-only` refuses to run when the main working copy has
// uncommitted changes (or untracked files) at paths the commit changes. That
// is common for beans: beans-serve writes Beans UI edits to main without
// committing, and Start Work copies the bean (including those edits) into the
// worktree, so the branch carries the very same content. For every path the
// squashed commit changes, the script compares main's uncommitted version with
// the branch's version:
//   - identical: main's change is redundant and is dropped (restored to HEAD,
//     or removed if untracked), so the fast-forward can apply the same content;
//   - different: nothing is touched; the script lists the paths and exits 1.
//
// Uncommitted changes at paths the commit doesn't change are left alone (the
// fast-forward keeps them). Works with bash 3.2 (macOS).
func integratePrepareMainScript(mainRepo string) string {
	return fmt.Sprintf(`MAIN=%s
SQUASH_SHA="${SQUASH_SHA:-$(git rev-parse HEAD)}"
BASE=$(git -C "$MAIN" rev-parse HEAD) || exit 1
drop=()
conflicts=()
while IFS= read -r -d '' P; do
  head=$(git -C "$MAIN" rev-parse -q --verify "HEAD:$P" 2>/dev/null)
  index=$(git -C "$MAIN" rev-parse -q --verify ":0:$P" 2>/dev/null)
  work=""
  if [ -f "$MAIN/$P" ]; then
    work=$(git -C "$MAIN" hash-object -- "$P") || exit 1
  fi
  if [ "$index" = "$head" ] && [ "$work" = "$head" ]; then
    continue # no uncommitted change in main
  fi
  branch=$(git -C "$MAIN" rev-parse -q --verify "$SQUASH_SHA:$P" 2>/dev/null)
  if [ "$work" = "$branch" ] && { [ "$index" = "$head" ] || [ "$index" = "$branch" ]; }; then
    drop+=("$P")
  else
    conflicts+=("$P")
  fi
done < <(git -C "$MAIN" diff -z --name-only --no-renames "$BASE" "$SQUASH_SHA")
if [ ${#conflicts[@]} -gt 0 ]; then
  echo "CONFLICT: the main repo ($MAIN) has uncommitted changes that differ from this branch's version of:"
  for P in "${conflicts[@]}"; do echo "  $P"; done
  echo "Nothing was changed. Ask the user to commit or discard these changes in the main repo (or to bring this branch's version in line), then integrate again."
  exit 1
fi
for P in ${drop[@]+"${drop[@]}"}; do
  if git -C "$MAIN" rev-parse -q --verify "HEAD:$P" >/dev/null 2>&1; then
    git -C "$MAIN" restore --source=HEAD --staged --worktree -- "$P" || exit 1
  else
    git -C "$MAIN" rm -q --cached --ignore-unmatch -- "$P" || exit 1
    rm -f -- "$MAIN/$P" || exit 1
  fi
  echo "Dropped the main repo's uncommitted copy of $P (identical to this branch's version)"
done
echo "Main is ready for the fast-forward."`, shellQuote(mainRepo))
}
