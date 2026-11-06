import { browser } from '$app/environment';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import { tick } from 'svelte';
import { beansStore, type Bean } from '$lib/beans.svelte';
import { foldState } from '$lib/foldState.svelte';
import { BOARD_STATUSES, planningFilter } from '$lib/planningFilter.svelte';
import {
  FOCUS_WIDE_KEY,
  backlogOrder,
  focusIdFromUrl,
  focusPosition,
  focusSourceFromPath,
  relativeHref,
  withFocus,
  type FocusPosition
} from '$lib/focusMode';

/**
 * Reactive state of focus mode (see focusMode.ts). The URL (`?focus=<id>`) is
 * the single source of truth for which bean is focused; this store derives
 * everything else from it and the bean store, and navigates via goto() so
 * SvelteKit's history (and thus browser back) stays consistent.
 */
class FocusMode {
  /** ID of the focused bean, or null when not in focus mode. */
  readonly id: string | null = $derived(browser ? focusIdFromUrl(page.url) : null);
  readonly active = $derived(this.id !== null);
  readonly bean: Bean | null = $derived(this.id ? (beansStore.get(this.id) ?? null) : null);

  /** The list prev/next steps through: the view focus mode was opened from. */
  readonly source = $derived(focusSourceFromPath(page.url.pathname));

  /** Bean IDs of that list, in its current order (live, with the active filter). */
  readonly order: string[] = $derived.by(() => {
    if (!this.active) return [];
    switch (this.source) {
      case 'backlog':
        return backlogOrder({
          sections: [planningFilter.backlogSections.todo, planningFilter.backlogSections.draft],
          childrenOf: (id) => beansStore.children(id),
          visible: planningFilter.tree?.visible ?? null,
          isCollapsed: (id) => foldState.isCollapsed(id)
        });
      case 'board':
        return BOARD_STATUSES.flatMap((s) => planningFilter.boardColumns[s].map((b) => b.id));
      default:
        return [];
    }
  });

  /** Where the focused bean was last seen in `order`, so stepping works after it leaves the list. */
  #lastIndex = $state<number | null>(null);

  readonly position: FocusPosition = $derived(focusPosition(this.order, this.id, this.#lastIndex));

  /** Wide reading column (persisted). */
  wide = $state(browser && localStorage.getItem(FOCUS_WIDE_KEY) === 'true');

  /** Element to return focus to on exit (the Focus button, a bean card, …). */
  #returnFocus: HTMLElement | null = null;

  /** Record the focused bean's position; call whenever `position` changes. */
  rememberPosition() {
    const index = this.position.index;
    if (index >= 0 && index !== this.#lastIndex) this.#lastIndex = index;
  }

  toggleWidth() {
    this.wide = !this.wide;
    if (browser) localStorage.setItem(FOCUS_WIDE_KEY, this.wide ? 'true' : 'false');
  }

  /** Href of the current URL with `id` focused (for links). */
  href(id: string): string {
    return relativeHref(withFocus(page.url, id));
  }

  /** Enter focus mode for a bean (adds a history entry, so browser back exits). */
  async enter(id: string) {
    if (this.id === id) return;
    if (!this.active) {
      const el = document.activeElement;
      this.#returnFocus = el instanceof HTMLElement && el !== document.body ? el : null;
      this.#lastIndex = null;
    }
    // window.location (not page.url) also has the params UIState mirrors without navigation.
    await goto(relativeHref(withFocus(new URL(window.location.href), id)), {
      keepFocus: true,
      noScroll: true,
      replaceState: this.active,
      state: { focusEntered: this.active ? !!page.state.focusEntered : true }
    });
  }

  /** Show another bean without leaving focus mode (replaces the history entry). */
  async show(id: string) {
    if (!this.active) return this.enter(id);
    if (id === this.id) return;
    await goto(relativeHref(withFocus(new URL(window.location.href), id)), {
      keepFocus: true,
      noScroll: true,
      replaceState: true,
      state: { focusEntered: !!page.state.focusEntered }
    });
  }

  step(direction: 'prev' | 'next') {
    const id = direction === 'prev' ? this.position.prevId : this.position.nextId;
    if (id) this.show(id);
  }

  /**
   * Leave focus mode. When we added the history entry, go back to it (so
   * back/forward stay in sync); otherwise (opened from a link or reload)
   * drop the param in place.
   */
  async exit() {
    if (!this.active) return;
    if (page.state.focusEntered) {
      history.back();
      return;
    }
    const url = withFocus(new URL(window.location.href), null);
    // Select the bean in the regular view. Set in the target URL (UIState's
    // `?bean=` param) rather than afterwards, which SvelteKit's own history
    // update at the end of the navigation would overwrite.
    if (this.bean) url.searchParams.set('bean', this.bean.id);
    await goto(relativeHref(url), {
      keepFocus: true,
      noScroll: true,
      replaceState: true
    });
  }

  /**
   * Restore focus after leaving focus mode (called by the layout once the
   * regular view is interactive again).
   */
  async restoreFocus() {
    const el = this.#returnFocus;
    this.#returnFocus = null;
    this.#lastIndex = null;
    await tick();
    const target =
      el?.isConnected && !el.closest('[inert]')
        ? el
        : document.querySelector<HTMLElement>('[data-focus-enter]');
    target?.focus({ preventScroll: true });
  }
}

export const focusMode = new FocusMode();
