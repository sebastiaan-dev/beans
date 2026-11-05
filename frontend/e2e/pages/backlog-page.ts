import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Page object for the backlog (list) view at /.
 */
export class BacklogPage {
  readonly beanItems: Locator;

  constructor(
    private page: Page,
    private baseURL: string
  ) {
    this.beanItems = page.locator('.bean-item');
  }

  /**
   * Navigate to the backlog page and wait for beans to load.
   * @param expectedCount If provided, wait until exactly this many beans are visible.
   */
  async goto(expectedCount?: number) {
    await this.page.goto(this.baseURL + '/');
    if (expectedCount !== undefined && expectedCount > 0) {
      await expect(this.beanItems).toHaveCount(expectedCount, { timeout: 10_000 });
    } else if (expectedCount === undefined) {
      await this.page.waitForSelector('.bean-item', { timeout: 10_000 });
    }
  }

  /** Get all visible bean titles in display order. */
  async getBeanTitles(): Promise<string[]> {
    const titles = await this.beanItems
      .locator('[role="button"] > div > span.text-sm')
      .allTextContents();
    return titles.map((t) => t.trim());
  }

  /** Get all visible bean statuses in display order. */
  async getBeanStatuses(): Promise<string[]> {
    const statuses = await this.beanItems
      .locator('[role="button"] > div > span.rounded-full')
      .allTextContents();
    return statuses.map((s) => s.trim());
  }

  /** Click on a bean by its title. */
  async selectBean(title: string) {
    await this.beanItems.filter({ hasText: title }).first().locator('[role="button"]').click();
  }

  /** Wait for a specific bean to appear. */
  async waitForBean(title: string) {
    await this.page.locator('.bean-item', { hasText: title }).waitFor({ timeout: 10_000 });
  }

  /** Wait for a bean to disappear from the list. */
  async waitForBeanGone(title: string) {
    await this.page
      .locator('.bean-item', { hasText: title })
      .waitFor({ state: 'detached', timeout: 10_000 });
  }

  /** Get the count of visible beans. */
  async count(): Promise<number> {
    return this.beanItems.count();
  }

  /** Get the .bean-item for a specific bean by title (uses data-bean-id for precision). */
  beanByTitle(title: string): Locator {
    return this.beanItems.filter({ hasText: title }).first();
  }

  /** The fold (collapse/expand) toggle of a bean with children, by title. */
  foldToggle(title: string): Locator {
    return this.page.getByRole('button', {
      name: new RegExp(`^(Expand|Collapse) ${escapeRegExp(title)} \\(\\d+ child(ren)?\\)$`)
    });
  }

  /** Collapse or expand a bean's children by clicking its fold toggle. */
  async toggleFold(title: string) {
    await this.foldToggle(title).click();
  }

  async collapseAll() {
    await this.page.getByRole('button', { name: 'Collapse all', exact: true }).click();
  }

  async expandAll() {
    await this.page.getByRole('button', { name: 'Expand all', exact: true }).click();
  }

  /**
   * Get the draggable card element for a bean, identified by title.
   * Each [draggable] div contains only its own card's content (not descendants),
   * so filtering by text gives us the exact bean's drag handle.
   */
  private draggableByTitle(title: string): Locator {
    return this.page.locator(
      `[draggable="true"]:has([role="button"] span.text-sm:text-is("${title}"))`
    );
  }

  /**
   * Drag a bean to reorder it above/below another bean, or onto it to reparent.
   *
   * The drop zones are: top 25% = above, middle 50% = reparent, bottom 25% = below.
   * We target 10%/90% for reorder and 50% for reparent to avoid zone boundaries.
   */
  async dragBean(
    sourceTitle: string,
    targetTitle: string,
    position: 'above' | 'below' | 'onto' = 'above'
  ) {
    const source = this.draggableByTitle(sourceTitle);
    const target = this.draggableByTitle(targetTitle);

    const targetBox = await target.boundingBox();
    if (!targetBox) throw new Error(`Target bean "${targetTitle}" not visible`);

    // Compute Y offset within the target card
    const yFraction = position === 'above' ? 0.1 : position === 'below' ? 0.9 : 0.5;
    const targetY = targetBox.height * yFraction;

    await source.dragTo(target, {
      targetPosition: { x: targetBox.width / 2, y: targetY }
    });
  }
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
