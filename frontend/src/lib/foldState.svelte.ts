import { browser } from '$app/environment';
import { FoldModel } from '$lib/fold';

/**
 * Reactive fold (collapse/expand) state for beans with children, persisted
 * per project in localStorage. See fold.ts for the underlying model.
 */
class FoldState {
  #model = new FoldModel(browser ? localStorage : null);
  // Bumped on every change so readers re-run; the model itself is not reactive.
  #version = $state(0);

  /** Load the fold state of a project (no-op if it is already loaded). */
  setProject(projectName: string) {
    if (this.#model.setProject(projectName)) this.#version++;
  }

  isCollapsed(id: string): boolean {
    void this.#version;
    return this.#model.isCollapsed(id);
  }

  get collapsedCount(): number {
    void this.#version;
    return this.#model.collapsed.size;
  }

  toggle(id: string) {
    if (this.#model.toggle(id)) this.#version++;
  }

  expand(id: string) {
    if (this.#model.setCollapsed(id, false)) this.#version++;
  }

  collapseAll(ids: Iterable<string>) {
    if (this.#model.collapseAll(ids)) this.#version++;
  }

  expandAll() {
    if (this.#model.expandAll()) this.#version++;
  }
}

export const foldState = new FoldState();
