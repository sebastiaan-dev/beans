/**
 * Pure helpers for folding (collapsing) beans with children in tree views such
 * as the backlog. The reactive store lives in foldState.svelte.ts.
 *
 * Fold state is stored as the set of *collapsed* bean IDs, so beans are
 * expanded by default and newly created parents never start out hidden.
 */

/** Minimal Storage interface, so the store can be tested without a browser. */
export type KeyValueStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

const STORAGE_PREFIX = 'beans-folded';

/** localStorage key holding the collapsed bean IDs of a project. */
export function foldStorageKey(projectName: string): string {
  return `${STORAGE_PREFIX}:${projectName}`;
}

/** Parse a persisted list of collapsed IDs, ignoring malformed data. */
export function parseCollapsed(raw: string | null): Set<string> {
  if (!raw) return new Set();
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return new Set();
    return new Set(value.filter((id): id is string => typeof id === 'string' && id !== ''));
  } catch {
    return new Set();
  }
}

/** Serialize collapsed IDs deterministically (sorted, for stable storage). */
export function serializeCollapsed(ids: Iterable<string>): string {
  return JSON.stringify([...ids].sort());
}

/** IDs of beans that have at least one child among `beans`. */
export function parentIds(
  beans: ReadonlyArray<{ id: string; parentId?: string | null }>
): string[] {
  const ids = new Set(beans.map((b) => b.id));
  const parents = new Set<string>();
  for (const bean of beans) {
    if (bean.parentId && ids.has(bean.parentId)) parents.add(bean.parentId);
  }
  return [...parents];
}

/**
 * Collapsed-ID set with optional per-project persistence. Mutations return
 * whether anything changed; the caller decides how to expose reactivity.
 */
export class FoldModel {
  #collapsed = new Set<string>();
  #storage: KeyValueStorage | null;
  #key: string | null = null;

  constructor(storage: KeyValueStorage | null) {
    this.#storage = storage;
  }

  get collapsed(): ReadonlySet<string> {
    return this.#collapsed;
  }

  /** The project whose fold state is loaded (null = in-memory only). */
  get storageKey(): string | null {
    return this.#key;
  }

  /**
   * Switch to the fold state of `projectName`, loading it from storage.
   * Returns false if that project is already active.
   */
  setProject(projectName: string): boolean {
    const key = foldStorageKey(projectName);
    if (key === this.#key) return false;
    this.#key = key;
    this.#collapsed = parseCollapsed(this.#read(key));
    return true;
  }

  isCollapsed(id: string): boolean {
    return this.#collapsed.has(id);
  }

  setCollapsed(id: string, collapsed: boolean): boolean {
    if (this.#collapsed.has(id) === collapsed) return false;
    const next = new Set(this.#collapsed);
    if (collapsed) next.add(id);
    else next.delete(id);
    this.#replace(next);
    return true;
  }

  toggle(id: string): boolean {
    return this.setCollapsed(id, !this.isCollapsed(id));
  }

  /**
   * Collapse exactly the given parents. Stale IDs of beans that no longer
   * exist are dropped as a side effect.
   */
  collapseAll(ids: Iterable<string>): boolean {
    return this.#replaceIfChanged(new Set(ids));
  }

  expandAll(): boolean {
    return this.#replaceIfChanged(new Set());
  }

  #replaceIfChanged(next: Set<string>): boolean {
    const same =
      next.size === this.#collapsed.size && [...next].every((id) => this.#collapsed.has(id));
    if (same) return false;
    this.#replace(next);
    return true;
  }

  #replace(next: Set<string>) {
    this.#collapsed = next;
    if (!this.#storage || !this.#key) return;
    try {
      if (next.size === 0) this.#storage.removeItem(this.#key);
      else this.#storage.setItem(this.#key, serializeCollapsed(next));
    } catch {
      // Storage full or unavailable: keep the in-memory state.
    }
  }

  #read(key: string): string | null {
    try {
      return this.#storage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  }
}
