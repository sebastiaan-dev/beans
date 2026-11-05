import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, findWorktrunk } from './fixtures';

/**
 * Info messages in the agent chat (posted by the server, e.g. when workspace
 * setup fails) are rendered as markdown.
 */
test.describe('Agent chat info messages', () => {
  // Unapproved worktrunk project hooks are a reliable way to fail setup.
  test.skip(!findWorktrunk(), 'worktrunk (wt) is not installed');
  test.use({ worktreeBackend: 'worktrunk' });

  test('renders the setup failure output as a code block', async ({ beans, page }) => {
    mkdirSync(join(beans.projectDir, '.config'), { recursive: true });
    // The hook contains markup, which must show up as text.
    writeFileSync(
      join(beans.projectDir, '.config', 'wt.toml'),
      `pre-start = "echo '<b>bold</b>' > /dev/null"\n`
    );
    beans.git(beans.projectDir, ['add', '.config/wt.toml']);
    beans.git(beans.projectDir, ['commit', '-m', 'add worktrunk hook']);

    await page.goto(beans.baseURL + '/');
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await sidebar.getByRole('button', { name: 'Create worktree' }).click();
    const dialog = page.getByRole('dialog', { name: 'New workspace' });
    await dialog.getByLabel('Name (git branch)').fill('hooked');
    await dialog.getByRole('button', { name: 'Create' }).click();
    await expect(page).toHaveURL(/\/workspace\/hooked/, { timeout: 20_000 });

    const info = page.getByTestId('agent-info-message').filter({ hasText: 'Workspace setup failed' });
    await expect(info).toBeVisible({ timeout: 15_000 });
    const code = info.locator('pre code');
    await expect(code).toContainText('worktrunk project hooks need your approval');
    await expect(code).toContainText("echo '<b>bold</b>' > /dev/null");
    await expect(info).not.toContainText('```');
    await expect(info.locator('b')).toHaveCount(0);
  });
});
