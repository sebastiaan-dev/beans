import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Page object for focus mode: the distraction-free reader for one bean
 * (`?focus=<id>`).
 */
export class FocusPage {
  readonly view: Locator;
  readonly article: Locator;
  readonly title: Locator;
  readonly meta: Locator;
  readonly body: Locator;
  readonly related: Locator;
  readonly position: Locator;
  readonly backButton: Locator;
  readonly prevButton: Locator;
  readonly nextButton: Locator;
  readonly editButton: Locator;
  readonly widthToggle: Locator;

  constructor(private page: Page) {
    this.view = page.locator('[data-focus-view]');
    this.article = this.view.locator('article[data-focus-article]');
    this.title = this.article.getByRole('heading', { level: 1 });
    this.meta = this.article.locator('[data-focus-meta]');
    this.body = this.article.locator('.focus-prose');
    this.related = this.article.locator('[data-focus-related-section]');
    this.position = this.view.locator('[data-focus-position]');
    const nav = this.view.getByRole('navigation', { name: 'Focus mode' });
    this.backButton = nav.getByRole('button', { name: 'Back' });
    this.prevButton = nav.getByRole('button', { name: 'Previous bean' });
    this.nextButton = nav.getByRole('button', { name: 'Next bean' });
    this.editButton = nav.getByRole('button', { name: 'Edit' });
    this.widthToggle = nav.getByRole('button', { name: 'Wide reading column' });
  }

  /** The Focus button in the regular bean detail pane. */
  enterButton(): Locator {
    return this.page.locator('[data-focus-enter]');
  }

  /** Wait until focus mode shows the bean with this title. */
  async expectShowing(title: string, id?: string) {
    await expect(this.view).toBeVisible();
    await expect(this.title).toHaveText(title);
    if (id) {
      await expect(this.article).toHaveAttribute('data-focus-article', id);
      await expect(this.page).toHaveURL(new RegExp(`[?&]focus=${id}(&|$)`));
    }
  }

  async expectClosed() {
    await expect(this.view).toHaveCount(0);
    await expect(this.page).not.toHaveURL(/[?&]focus=/);
  }

  /** A link in the Related section, by the target bean's ID. */
  relatedLink(id: string): Locator {
    return this.related.locator(`[data-focus-related="${id}"]`);
  }
}
