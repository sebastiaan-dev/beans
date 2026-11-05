import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Page object for the left sidebar (navigation + workspaces), which can be
 * collapsed to a narrow icon rail.
 */
export class SidebarPage {
  readonly nav: Locator;

  constructor(private page: Page) {
    this.nav = page.getByRole('navigation', { name: 'Sidebar' });
  }

  get collapseButton(): Locator {
    return this.nav.getByRole('button', { name: 'Collapse sidebar' });
  }

  get expandButton(): Locator {
    return this.nav.getByRole('button', { name: 'Expand sidebar' });
  }

  async collapse() {
    await this.collapseButton.click();
    await this.expectCollapsed(true);
  }

  async expand() {
    await this.expandButton.click();
    await this.expectCollapsed(false);
  }

  async expectCollapsed(collapsed: boolean) {
    await expect(this.nav).toHaveAttribute('data-collapsed', String(collapsed));
  }

  /** Width of the sidebar in px. */
  async width(): Promise<number> {
    const box = await this.nav.boundingBox();
    if (!box) throw new Error('Sidebar not visible');
    return box.width;
  }

  /** Workspace button in the collapsed rail (labelled by the workspace name). */
  railWorkspace(name: string): Locator {
    return this.nav.getByRole('button', { name, exact: true });
  }

  /** Create a workspace via the sidebar and return its ID (from the URL). */
  async createWorkspace(): Promise<string> {
    const before = this.page.url();
    await this.nav.getByRole('button', { name: 'Create worktree' }).click();
    await expect(this.page).not.toHaveURL(before, { timeout: 10_000 });
    await expect(this.page).toHaveURL(/\/workspace\/(?!__central__)[^/?]+/, { timeout: 10_000 });
    return new URL(this.page.url()).pathname.split('/')[2];
  }
}
