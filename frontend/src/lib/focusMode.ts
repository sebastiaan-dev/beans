/**
 * Pure helpers for focus mode: a distraction-free reader for a single bean.
 * The reactive store lives in focusMode.svelte.ts, the view in FocusView.svelte.
 *
 * Focus mode is driven by the `?focus=<id>` URL param on any view, so focused
 * beans are linkable/bookmarkable and browser back leaves focus mode.
 */

import type { Bean } from '$lib/beans.svelte';

/** URL param holding the focused bean's ID. */
export const FOCUS_URL_PARAM = 'focus';

/** localStorage key of the reading-width preference. */
export const FOCUS_WIDE_KEY = 'beans-focus-wide';

/** The focused bean's ID in a URL, or null when not in focus mode. */
export function focusIdFromUrl(url: URL): string | null {
  const id = url.searchParams.get(FOCUS_URL_PARAM)?.trim();
  return id ? id : null;
}

/** A copy of `url` with focus mode set to `id` (or left when `id` is null). */
export function withFocus(url: URL, id: string | null): URL {
  const next = new URL(url);
  if (id) {
    next.searchParams.set(FOCUS_URL_PARAM, id);
  } else {
    next.searchParams.delete(FOCUS_URL_PARAM);
  }
  return next;
}

/** Relative href (path + query + hash) of `url`, for links and goto(). */
export function relativeHref(url: URL): string {
  return url.pathname + url.search + url.hash;
}

/** The list prev/next steps through, derived from the view focus mode was opened from. */
export type FocusSource = 'backlog' | 'board' | 'none';

/** Which list a pathname shows (workspace views have no bean list to step through). */
export function focusSourceFromPath(pathname: string): FocusSource {
  if (pathname === '/planning/board') return 'board';
  if (pathname === '/planning' || pathname === '/planning/' || pathname === '/') return 'backlog';
  return 'none';
}

// ── Keyboard ─────────────────────────────────────────────

interface KeyEvent {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey?: boolean;
  defaultPrevented?: boolean;
  isComposing?: boolean;
  target?: EventTarget | null;
}

/** Whether keys typed at `target` are text input (inputs, editors, terminals). */
export function isTypingTarget(target: EventTarget | null | undefined): boolean {
  const el = target as {
    tagName?: string;
    isContentEditable?: boolean;
    type?: string;
    closest?: (selector: string) => unknown;
  } | null;
  if (!el || typeof el !== 'object') return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName?.toUpperCase();
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') {
    // Checkboxes, radios and buttons don't take text.
    const type = (el.type ?? 'text').toLowerCase();
    return !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'].includes(
      type
    );
  }
  if (typeof el.closest === 'function') {
    return !!el.closest('[contenteditable=""], [contenteditable="true"], [role="textbox"], .xterm');
  }
  return false;
}

/** Whether a keydown is a plain (unmodified) key press that may trigger a single-key shortcut. */
function isPlainKey(e: KeyEvent): boolean {
  if (e.defaultPrevented || e.isComposing) return false;
  if (e.metaKey || e.ctrlKey || e.altKey) return false;
  return !isTypingTarget(e.target);
}

/** Key that enters focus mode for the selected bean. */
export const FOCUS_ENTER_KEY = 'f';

/** Whether a keydown (outside focus mode) enters focus mode: plain `f`, not while typing. */
export function isFocusEnterShortcut(e: KeyEvent): boolean {
  return isPlainKey(e) && !e.shiftKey && e.key === FOCUS_ENTER_KEY;
}

export type FocusAction = 'exit' | 'prev' | 'next' | 'edit' | 'toggleWidth';

/**
 * The focus-mode action for a keydown, or null.
 *
 *   Esc        exit          ← / k   previous bean     → / j   next bean
 *   e          edit          w       toggle reading width
 *
 * Single letters are used (not Space/Home/…), so scrolling keys keep working;
 * nothing fires while typing in an input or editor.
 */
export function focusActionForKey(e: KeyEvent): FocusAction | null {
  if (e.defaultPrevented || e.isComposing) return null;
  if (e.key === 'Escape') {
    return e.metaKey || e.ctrlKey || e.altKey || e.shiftKey || isTypingTarget(e.target)
      ? null
      : 'exit';
  }
  if (!isPlainKey(e) || e.shiftKey) return null;
  switch (e.key) {
    case 'ArrowLeft':
    case 'k':
      return 'prev';
    case 'ArrowRight':
    case 'j':
      return 'next';
    case 'e':
      return 'edit';
    case 'w':
      return 'toggleWidth';
    default:
      return null;
  }
}

// ── Prev/next order ──────────────────────────────────────

export interface FocusPosition {
  /** Index of the bean in the list, or -1 when it isn't in it (anymore). */
  index: number;
  total: number;
  prevId: string | null;
  nextId: string | null;
}

/**
 * Neighbours of `id` in `order`. When the bean is not in the list (e.g. its
 * status changed so it left the backlog), `lastIndex` — where it was — keeps
 * stepping sensible: next is the bean that took its place, prev the one
 * before it.
 */
export function focusPosition(
  order: readonly string[],
  id: string | null,
  lastIndex: number | null = null
): FocusPosition {
  const total = order.length;
  const index = id ? order.indexOf(id) : -1;
  if (index >= 0) {
    return {
      index,
      total,
      prevId: index > 0 ? order[index - 1] : null,
      nextId: index < total - 1 ? order[index + 1] : null
    };
  }
  if (lastIndex === null || lastIndex < 0 || total === 0) {
    return { index: -1, total, prevId: null, nextId: null };
  }
  const at = Math.min(lastIndex, total);
  return {
    index: -1,
    total,
    prevId: at > 0 ? order[at - 1] : null,
    nextId: at < total ? order[at] : null
  };
}

export interface BacklogOrderInput {
  /** Root beans per section, in display order (e.g. [todoRoots, draftRoots]), already filtered. */
  sections: readonly (readonly Bean[])[];
  /** Children of a bean in display order (unfiltered). */
  childrenOf: (id: string) => readonly Bean[];
  /** Beans visible under the active filter, or null when unfiltered. */
  visible: ReadonlySet<string> | null;
  /** Whether a bean's children are folded away. */
  isCollapsed: (id: string) => boolean;
}

/**
 * Bean IDs in the order the backlog renders them: each section's roots
 * depth-first with their visible children, skipping folded subtrees (folding
 * is ignored while a filter is active, like BeanItem does).
 */
export function backlogOrder({
  sections,
  childrenOf,
  visible,
  isCollapsed
}: BacklogOrderInput): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  const visit = (bean: Bean) => {
    if (seen.has(bean.id)) return; // defensive: parent cycles
    seen.add(bean.id);
    out.push(bean.id);
    const children = childrenOf(bean.id).filter((c) => !visible || visible.has(c.id));
    if (children.length === 0 || (!visible && isCollapsed(bean.id))) return;
    for (const child of children) visit(child);
  };
  for (const roots of sections) {
    for (const root of roots) visit(root);
  }
  return out;
}

// ── Related beans ────────────────────────────────────────

export interface RelatedBeans {
  parent: Bean | null;
  children: Bean[];
  blocking: Bean[];
  blockedBy: Bean[];
}

/**
 * Relations of a bean. `beans` (display order) is scanned for children and
 * inverse links; `get` resolves IDs the bean itself references (defaults to a
 * lookup in `beans`, pass one that also finds archived beans to keep e.g. an
 * archived parent). Blocking and blocked-by are symmetric: the bean's own
 * list plus the other beans' inverse links, like the GraphQL fields.
 */
export function relatedBeans(
  bean: Bean,
  beans: readonly Bean[],
  get?: (id: string) => Bean | undefined
): RelatedBeans {
  const byId = new Map(beans.map((b) => [b.id, b]));
  const lookup = get ?? ((id: string) => byId.get(id));
  const resolve = (ids: readonly string[]) =>
    [...new Set(ids)]
      .filter((id) => id !== bean.id)
      .map((id) => lookup(id))
      .filter((b): b is Bean => !!b);
  return {
    parent: bean.parentId ? (lookup(bean.parentId) ?? null) : null,
    children: beans.filter((b) => b.parentId === bean.id),
    blocking: resolve([
      ...bean.blockingIds,
      ...beans.filter((b) => b.blockedByIds.includes(bean.id)).map((b) => b.id)
    ]),
    blockedBy: resolve([
      ...bean.blockedByIds,
      ...beans.filter((b) => b.blockingIds.includes(bean.id)).map((b) => b.id)
    ])
  };
}

export function hasRelated(r: RelatedBeans): boolean {
  return !!r.parent || r.children.length + r.blocking.length + r.blockedBy.length > 0;
}
