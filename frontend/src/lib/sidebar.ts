/**
 * Pure helpers for the collapsible left sidebar. The reactive store lives in
 * sidebarState.svelte.ts.
 */

import type { KeyValueStorage } from '$lib/fold';

export const SIDEBAR_COLLAPSED_KEY = 'beans-sidebar-collapsed';

/** Width (px) of the collapsed icon rail. */
export const SIDEBAR_RAIL_WIDTH = 52;

/** Below this viewport width the sidebar starts collapsed unless the user chose otherwise. */
export const SIDEBAR_AUTO_COLLAPSE_WIDTH = 768;

/** Human-readable shortcut label, e.g. for tooltips. */
export function sidebarShortcutLabel(isMac: boolean): string {
  return isMac ? '⌘\\' : 'Ctrl+\\';
}

/**
 * Initial collapsed state: the persisted preference if there is one, otherwise
 * collapsed on narrow viewports so the main content keeps its space.
 */
export function initialSidebarCollapsed(
  storage: KeyValueStorage | null,
  viewportWidth: number
): boolean {
  let saved: string | null = null;
  try {
    saved = storage?.getItem(SIDEBAR_COLLAPSED_KEY) ?? null;
  } catch {
    saved = null;
  }
  if (saved === 'true') return true;
  if (saved === 'false') return false;
  return viewportWidth > 0 && viewportWidth < SIDEBAR_AUTO_COLLAPSE_WIDTH;
}

export function persistSidebarCollapsed(storage: KeyValueStorage | null, collapsed: boolean) {
  try {
    storage?.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? 'true' : 'false');
  } catch {
    // Storage unavailable: the state just won't survive a reload.
  }
}

interface ShortcutEvent {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  defaultPrevented?: boolean;
  target?: EventTarget | null;
}

/**
 * Whether a keydown toggles the sidebar: Cmd+\ (macOS) / Ctrl+\ (elsewhere).
 *
 * Cmd/Ctrl+B is avoided because it means "bold" in the bean editor. Events
 * from embedded terminals are ignored, where Ctrl+\ sends SIGQUIT.
 */
export function isSidebarToggleShortcut(e: ShortcutEvent): boolean {
  if (e.defaultPrevented) return false;
  if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return false;
  if (e.key !== '\\') return false;
  const target = e.target as { closest?: (selector: string) => unknown } | null | undefined;
  if (typeof target?.closest === 'function' && target.closest('.xterm')) return false;
  return true;
}

/**
 * Short label for a workspace in the collapsed rail: initials of the first two
 * words (split on whitespace, dashes, underscores, slashes and dots), or the
 * first two characters of a single word.
 */
export function workspaceInitials(label: string): string {
  const words = label
    .split(/[\s\-_/.]+/)
    .map((w) => w.replace(/[^\p{L}\p{N}]/gu, ''))
    .filter(Boolean);
  if (words.length === 0) return '?';
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase();
  return (([...words[0]][0] ?? '') + ([...words[1]][0] ?? '')).toUpperCase();
}
