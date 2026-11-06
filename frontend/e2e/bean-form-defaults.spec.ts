import { test, expect } from './fixtures';

test.describe('New bean form defaults', () => {
  test.describe('with custom defaults in .beans.yml', () => {
    test.use({ beansDefaults: { status: 'draft', type: 'feature' } });

    test('pre-selects the configured default type and status', async ({ beans, page }) => {
      await page.goto(beans.baseURL + '/');
      await page.getByRole('button', { name: '+ New Bean' }).click();

      await expect(page.locator('#bean-type')).toHaveValue('feature');
      await expect(page.locator('#bean-status')).toHaveValue('draft');
    });

    test('creates beans with the configured defaults when untouched', async ({ beans, page }) => {
      await page.goto(beans.baseURL + '/');
      await page.getByRole('button', { name: '+ New Bean' }).click();
      await page.locator('#bean-title').fill('Defaulted Bean');
      await page.getByRole('button', { name: 'Create Bean' }).click();

      await expect(page.locator('#bean-title')).not.toBeVisible();
      const data = await beans.graphql<{
        beans: { title: string; type: string; status: string }[];
      }>('{ beans { title type status } }');
      expect(data.beans).toEqual([{ title: 'Defaulted Bean', type: 'feature', status: 'draft' }]);
    });

    test('creates beans with the configured defaults when submitted before config loads', async ({
      beans,
      page
    }) => {
      // Hold back the Config query until the bean has been created.
      let releaseConfig!: () => void;
      const configHeld = new Promise<void>((resolve) => (releaseConfig = resolve));
      await page.route('**/api/graphql**', async (route) => {
        const request = route.request();
        const query =
          new URL(request.url()).searchParams.get('query') ?? request.postDataJSON()?.query ?? '';
        if (query.includes('query Config')) {
          await configHeld;
        }
        await route.fallback();
      });

      await page.goto(beans.baseURL + '/');
      await page.getByRole('button', { name: '+ New Bean' }).click();
      await page.locator('#bean-title').fill('Early Bean');
      await page.getByRole('button', { name: 'Create Bean' }).click();
      await expect(page.locator('#bean-title')).not.toBeVisible();
      releaseConfig();

      const data = await beans.graphql<{
        beans: { title: string; type: string; status: string }[];
      }>('{ beans { title type status } }');
      expect(data.beans).toEqual([{ title: 'Early Bean', type: 'feature', status: 'draft' }]);
    });

    test('creates beans with explicitly chosen type and status', async ({ beans, page }) => {
      await page.goto(beans.baseURL + '/');
      await page.getByRole('button', { name: '+ New Bean' }).click();
      await page.locator('#bean-title').fill('Chosen Bean');
      await page.locator('#bean-type').selectOption('bug');
      await page.locator('#bean-status').selectOption('todo');
      await page.getByRole('button', { name: 'Create Bean' }).click();

      await expect(page.locator('#bean-title')).not.toBeVisible();
      const data = await beans.graphql<{
        beans: { title: string; type: string; status: string }[];
      }>('{ beans { title type status } }');
      expect(data.beans).toEqual([{ title: 'Chosen Bean', type: 'bug', status: 'todo' }]);
    });
  });

  test('falls back to the built-in defaults without custom config', async ({ beans, page }) => {
    await page.goto(beans.baseURL + '/');
    await page.getByRole('button', { name: '+ New Bean' }).click();

    await expect(page.locator('#bean-type')).toHaveValue('task');
    await expect(page.locator('#bean-status')).toHaveValue('todo');
  });
});
