import { test, expect } from './fixtures';

test.describe('Workflow action buttons', () => {
  test('draft bean shows Todo and Scrap buttons', async ({ beans, backlogPage, page }) => {
    beans.create('Draft Bean', { status: 'draft', type: 'task' });

    await backlogPage.goto(1);
    await backlogPage.selectBean('Draft Bean');

    const detail = page.locator('h1', { hasText: 'Draft Bean' }).locator('..');

    await expect(detail.getByRole('button', { name: 'Todo' })).toBeVisible();
    await expect(detail.getByRole('button', { name: 'Scrap' })).toBeVisible();
    await expect(detail.getByRole('button', { name: 'Complete' })).not.toBeVisible();
  });

  test('todo bean shows Scrap button (no Start Work without agent)', async ({
    beans,
    backlogPage,
    page
  }) => {
    beans.create('Todo Bean', { status: 'todo', type: 'task' });

    await backlogPage.goto(1);
    await backlogPage.selectBean('Todo Bean');

    const detail = page.locator('h1', { hasText: 'Todo Bean' }).locator('..');

    await expect(detail.getByRole('button', { name: 'Scrap' })).toBeVisible();
    await expect(detail.getByRole('button', { name: 'Todo' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Complete' })).not.toBeVisible();
  });

  test('in-progress bean shows Complete and Scrap buttons', async ({ beans, boardPage, page }) => {
    beans.create('Active Bean', { status: 'in-progress', type: 'task' });

    await boardPage.goto();
    await boardPage.waitForBeanInColumn('Active Bean', 'in-progress');
    // Click the bean in the board to select it
    await page
      .locator('[data-status="in-progress"] [role="listitem"]', { hasText: 'Active Bean' })
      .locator('[role="button"]')
      .click();

    const detail = page.locator('h1', { hasText: 'Active Bean' }).locator('..');

    await expect(detail.getByRole('button', { name: 'Complete' })).toBeVisible();
    await expect(detail.getByRole('button', { name: 'Scrap' })).toBeVisible();
    await expect(detail.getByRole('button', { name: 'Todo' })).not.toBeVisible();
  });

  test('completed bean shows no workflow buttons', async ({ beans, boardPage, page }) => {
    beans.create('Done Bean', { status: 'completed', type: 'task' });

    await boardPage.goto();
    await boardPage.waitForBeanInColumn('Done Bean', 'completed');
    await page
      .locator('[data-status="completed"] [role="listitem"]', { hasText: 'Done Bean' })
      .locator('[role="button"]')
      .click();

    const detail = page.locator('h1', { hasText: 'Done Bean' }).locator('..');

    await expect(detail.getByRole('button', { name: 'Todo' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Scrap' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Complete' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Start Work' })).not.toBeVisible();
  });

  test('scrapped bean shows no workflow buttons', async ({ beans, page }) => {
    const id = beans.create('Scrapped Bean', { status: 'scrapped', type: 'task' });

    // Navigate directly with bean param since scrapped beans don't appear in any view
    await page.goto(`${beans.baseURL}/?bean=${id}`);
    await expect(page.locator('h1', { hasText: 'Scrapped Bean' })).toBeVisible({ timeout: 10_000 });

    const detail = page.locator('h1', { hasText: 'Scrapped Bean' }).locator('..');

    await expect(detail.getByRole('button', { name: 'Todo' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Scrap' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Complete' })).not.toBeVisible();
  });

  test('Todo button moves draft bean to todo status', async ({ beans, backlogPage, page }) => {
    beans.create('My Draft', { status: 'draft', type: 'task' });

    await backlogPage.goto(1);
    await backlogPage.selectBean('My Draft');

    const detail = page.locator('h1', { hasText: 'My Draft' }).locator('..');

    await detail.getByRole('button', { name: 'Todo' }).click();

    // Workflow buttons should update to todo state
    await expect(detail.getByRole('button', { name: 'Scrap' })).toBeVisible({ timeout: 5000 });
    await expect(detail.getByRole('button', { name: 'Todo' })).not.toBeVisible();
  });

  test('Complete button moves in-progress bean to completed', async ({
    beans,
    boardPage,
    page
  }) => {
    beans.create('Active Task', { status: 'in-progress', type: 'task' });

    await boardPage.goto();
    await boardPage.waitForBeanInColumn('Active Task', 'in-progress');
    await page
      .locator('[data-status="in-progress"] [role="listitem"]', { hasText: 'Active Task' })
      .locator('[role="button"]')
      .click();

    const detail = page.locator('h1', { hasText: 'Active Task' }).locator('..');

    await detail.getByRole('button', { name: 'Complete' }).click();

    // No workflow buttons should remain, Archive should appear
    await expect(detail.getByRole('button', { name: 'Archive' })).toBeVisible({ timeout: 5000 });
    await expect(detail.getByRole('button', { name: 'Complete' })).not.toBeVisible();
    await expect(detail.getByRole('button', { name: 'Scrap' })).not.toBeVisible();
  });

  test('Scrap button moves bean to scrapped', async ({ beans, backlogPage, page }) => {
    beans.create('Unwanted Bean', { status: 'todo', type: 'task' });

    await backlogPage.goto(1);
    await backlogPage.selectBean('Unwanted Bean');

    const detail = page.locator('h1', { hasText: 'Unwanted Bean' }).locator('..');

    await detail.getByRole('button', { name: 'Scrap' }).click();

    // No workflow buttons should remain, Archive should appear
    await expect(detail.getByRole('button', { name: 'Archive' })).toBeVisible({ timeout: 5000 });
    await expect(detail.getByRole('button', { name: 'Scrap' })).not.toBeVisible();
  });
});
