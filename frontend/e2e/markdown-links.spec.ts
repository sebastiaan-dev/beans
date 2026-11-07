import { test, expect } from './fixtures';

test.describe('Markdown links', () => {
  test('external links have target="_blank" and rel="noopener noreferrer"', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Link Test Bean', {
      status: 'todo',
      type: 'task'
    });

    // Update the bean body with a markdown link and a bare URL
    beans.run([
      'update',
      id,
      '--body-append',
      'Visit [Example](https://example.com) or https://example.org for more info.'
    ]);

    await backlogPage.goto(1);
    await backlogPage.selectBean('Link Test Bean');

    // Wait for the bean detail body to render
    const body = page.locator('.bean-body');
    await expect(body).toBeVisible({ timeout: 10_000 });

    // Check the explicit markdown link
    const markdownLink = body.locator('a[href="https://example.com"]');
    await expect(markdownLink).toHaveAttribute('target', '_blank');
    await expect(markdownLink).toHaveAttribute('rel', 'noopener noreferrer');

    // Check the autolinked bare URL
    const autoLink = body.locator('a[href="https://example.org"]');
    await expect(autoLink).toHaveAttribute('target', '_blank');
    await expect(autoLink).toHaveAttribute('rel', 'noopener noreferrer');
  });

  test('bean IDs of existing beans link to the bean, whatever the prefix', async ({
    beans,
    backlogPage,
    page
  }) => {
    // The e2e project's prefix is not the default `beans-`.
    const target = beans.create('Link Target', { status: 'todo' });
    expect(target.startsWith('beans-e2e-')).toBe(true);
    const source = beans.create('Link Source', { status: 'todo' });
    beans.run(['update', source, '--body', `See ${target}, not beans-zzzz or \`${target}\`.`]);

    await backlogPage.goto(2);
    await backlogPage.selectBean('Link Source');
    const body = page.locator('.bean-body');
    await expect(body).toContainText('beans-zzzz');
    const links = body.locator('[data-bean-id]');
    await expect(links).toHaveCount(1);
    await expect(links).toHaveAttribute('data-bean-id', target);
    await expect(links).toHaveText(target);
    await expect(body.locator('code')).toHaveText(target);

    await links.click();
    await expect(page).toHaveURL(new RegExp(`[?&]bean=${target}`));

    // The link goes away when the bean is deleted.
    await backlogPage.selectBean('Link Source');
    await expect(body.locator('[data-bean-id]')).toHaveCount(1);
    beans.run(['delete', '--force', target]);
    await backlogPage.waitForBeanGone('Link Target');
    await expect(body.locator('[data-bean-id]')).toHaveCount(0);
    await expect(body).toContainText(`See ${target},`);
  });
});
