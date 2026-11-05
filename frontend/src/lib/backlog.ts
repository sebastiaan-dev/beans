import type { Bean } from '$lib/beans.svelte';

/** Statuses that make up the backlog view's sections. */
export const BACKLOG_STATUSES = ['todo', 'draft'] as const;

export function isBacklogStatus(status: string): boolean {
  return (BACKLOG_STATUSES as readonly string[]).includes(status);
}

/**
 * Whether a bean is rendered as a root of a backlog section.
 *
 * The backlog renders each root together with its full subtree (children of
 * every status), so a backlog bean only needs to be a root when none of its
 * ancestors is itself in the backlog. Otherwise its topmost backlog ancestor
 * is a root and the bean shows up nested inside that ancestor's subtree.
 *
 * Without this, a todo/draft bean whose parent is in-progress, completed,
 * scrapped, archived or unknown would be unreachable in the backlog (it is
 * still shown on the board) — e.g. follow-ups an agent files under the bean it
 * is working on (hmans/beans#181).
 *
 * @param byId lookup of visible (non-archived) beans; ancestors that are
 *   missing from it end the parent chain, just like when rendering children.
 */
export function isBacklogRoot(bean: Bean, byId: ReadonlyMap<string, Bean>): boolean {
  if (!isBacklogStatus(bean.status)) return false;

  const seen = new Set<string>([bean.id]);
  let parentId = bean.parentId;
  while (parentId && !seen.has(parentId)) {
    const parent = byId.get(parentId);
    if (!parent) break;
    if (isBacklogStatus(parent.status)) return false;
    seen.add(parentId);
    parentId = parent.parentId;
  }
  return true;
}

/** Roots of the backlog section for `status`, preserving the input order. */
export function backlogRoots(beans: Bean[], status: string): Bean[] {
  const byId = new Map(beans.map((b) => [b.id, b]));
  return beans.filter((b) => b.status === status && isBacklogRoot(b, byId));
}
