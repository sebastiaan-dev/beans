import { browser } from '$app/environment';
import { initialSidebarCollapsed, persistSidebarCollapsed } from '$lib/sidebar';

/** Collapsed state of the left sidebar, persisted in localStorage. */
class SidebarState {
  collapsed = $state(browser ? initialSidebarCollapsed(localStorage, window.innerWidth) : false);

  setCollapsed(collapsed: boolean) {
    this.collapsed = collapsed;
    persistSidebarCollapsed(browser ? localStorage : null, collapsed);
  }

  toggle() {
    this.setCollapsed(!this.collapsed);
  }
}

export const sidebarState = new SidebarState();
