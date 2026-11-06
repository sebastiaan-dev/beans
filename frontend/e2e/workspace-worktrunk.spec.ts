import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, findWorktrunk } from './fixtures';

/**
 * Web UI with the worktrunk backend (`worktree.backend: worktrunk`), using a
 * real `wt` with an isolated configuration (see fixtures.ts createWorktrunk).
 */
test.describe('Workspaces with the worktrunk backend', () => {
  test.skip(!findWorktrunk(), 'worktrunk (wt) is not installed');
  test.use({ worktreeBackend: 'worktrunk' });

  test('create a workspace from the UI with a typed name: it becomes the branch', async ({
    beans,
    page
  }) => {
    await page.goto(beans.baseURL + '/');
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await sidebar.getByRole('button', { name: 'Create worktree' }).click();

    const dialog = page.getByRole('dialog', { name: 'New workspace' });
    await expect(dialog).toBeVisible();
    const input = dialog.getByLabel('Name (git branch)');
    await input.fill('feat-login');
    await expect(dialog.locator('[data-branch-preview]')).toHaveText('feat-login');
    await dialog.getByRole('button', { name: 'Create' }).click();

    await expect(page).toHaveURL(/\/workspace\/feat-login/, { timeout: 20_000 });
    await expect(dialog).not.toBeVisible();
    await expect(sidebar.locator('button[data-workspace-id="feat-login"]')).toContainText('feat-login');
    await expect(page.locator('[data-workspace-branch]')).toHaveText('feat-login');

    const [wt] = await beans.getWorktrees();
    expect(wt).toMatchObject({ id: 'feat-login', branch: 'feat-login' });
    // The branch is exactly what was typed (no beans/ prefix).
    expect(beans.git(beans.projectDir, ['branch', '--list', 'feat-login']).trim()).toContain('feat-login');
    expect(beans.git(beans.projectDir, ['branch', '--list', 'beans/*']).trim()).toBe('');
    // The path (shown on hover) is worktrunk's template location.
    await expect(sidebar.locator('button[data-workspace-id="feat-login"]')).toHaveAttribute(
      'title',
      new RegExp(`Branch: feat-login\\nPath: ${wt.path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`)
    );
    expect(wt.path).toMatch(/\/trees\/feat-login$/);
  });

  test('invalid names are rejected client-side with a suggestion', async ({ beans, page }) => {
    await page.goto(beans.baseURL + '/');
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await sidebar.getByRole('button', { name: 'Create worktree' }).click();

    const dialog = page.getByRole('dialog', { name: 'New workspace' });
    const input = dialog.getByLabel('Name (git branch)');
    const create = dialog.getByRole('button', { name: 'Create' });

    await input.fill('fix/foo');
    const error = dialog.locator('[data-name-error]');
    await expect(error).toContainText("Invalid name: it must start with a letter or digit");
    await expect(create).toBeDisabled();

    await input.fill('a..b');
    await expect(error).toContainText("must not contain '..'");
    await input.fill('');
    await expect(error).toContainText('must not be empty');

    await input.fill('Fix Foo');
    await dialog.getByRole('button', { name: 'Use "fix-foo"' }).click();
    await expect(input).toHaveValue('fix-foo');
    await expect(error).not.toBeVisible();
    await expect(create).toBeEnabled();

    // Nothing was created; Escape closes the dialog.
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
    expect(await beans.getWorktrees()).toEqual([]);
  });

  test('worktrees created and removed with wt show up live; an open one navigates away', async ({
    beans,
    page
  }) => {
    await page.goto(beans.baseURL + '/');
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await expect(sidebar.getByText('Workspaces')).toBeVisible();

    beans.wt(['switch', '--create', 'fix/ext', '--no-cd', '--no-hooks']);

    // Arbitrary branch names get a sanitized ID; the label is the branch.
    const item = sidebar.locator('button[data-workspace-id="fix-ext"]');
    await expect(item).toBeVisible({ timeout: 15_000 });
    await expect(item).toContainText('fix/ext');
    await expect(item).toHaveAttribute('title', /Branch: fix\/ext\nPath: .*\/trees\/fix-ext/);

    await item.click();
    await expect(page).toHaveURL(/\/workspace\/fix-ext/);
    await expect(page.locator('[data-workspace-branch]')).toHaveText('fix/ext');

    beans.wt(['remove', 'fix/ext', '--foreground', '--no-hooks']);

    await expect(page).toHaveURL(/\/planning/, { timeout: 15_000 });
    await expect(page.locator('[data-toast]')).toContainText('Workspace "fix/ext" was removed');
    await expect(item).not.toBeVisible();
  });

  test('detached worktrees are listed with a detached indicator', async ({ beans, page }) => {
    await page.goto(beans.baseURL + '/');
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await expect(sidebar.getByText('Workspaces')).toBeVisible();

    const trees = join(beans.worktreeRoot, 'trees');
    mkdirSync(trees, { recursive: true });
    beans.git(beans.projectDir, ['worktree', 'add', '--detach', join(trees, 'scratch')]);

    const item = sidebar.locator('button[data-workspace-id="scratch"]');
    await expect(item).toBeVisible({ timeout: 15_000 });
    await expect(item).toContainText('scratch');
    await expect(item.locator('[data-detached]')).toHaveText('detached');
    await expect(item).toHaveAttribute('title', /Detached HEAD/);
  });

  test('destroying a workspace from the UI removes the worktree', async ({ beans, page }) => {
    const wt = await beans.createWorktree('to-destroy');
    expect(wt.branch).toBe('to-destroy');

    await page.goto(`${beans.baseURL}/workspace/to-destroy`);
    await page.getByRole('button', { name: 'Close Workspace' }).click();
    const modal = page.getByText(/Worktrunk's pre-remove\/post-remove hooks run/);
    await expect(modal).toBeVisible();
    await page.getByRole('button', { name: 'Destroy', exact: true }).click();

    await expect(page).toHaveURL(/\/planning/, { timeout: 10_000 });
    const sidebar = page.getByRole('navigation', { name: 'Sidebar' });
    await expect(sidebar.locator('button[data-workspace-id="to-destroy"]')).not.toBeVisible({
      timeout: 15_000
    });
    await expect.poll(() => beans.getWorktrees(), { timeout: 10_000 }).toEqual([]);
    expect(existsSync(wt.path)).toBe(false);
    // No commits on the branch, so it is merged and worktrunk deleted it.
    expect(beans.git(beans.projectDir, ['branch', '--list', 'to-destroy']).trim()).toBe('');
    // We navigated away ourselves: no "was removed" notification.
    await expect(page.locator('[data-toast]')).toHaveCount(0);
  });

  test('unapproved project hooks fail setup with approval instructions', async ({ beans, page }) => {
    const marker = join(beans.projectDir, 'hook-ran');
    mkdirSync(join(beans.projectDir, '.config'), { recursive: true });
    writeFileSync(
      join(beans.projectDir, '.config', 'wt.toml'),
      `pre-start = "touch ${marker}"\n`
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

    const banner = page.locator('[data-setup-status="failed"]');
    await expect(banner).toBeVisible({ timeout: 15_000 });
    await expect(banner).toContainText('Workspace setup failed');
    await expect(banner).toContainText('worktrunk project hooks need your approval');
    await expect(banner).toContainText(`touch ${marker}`);
    // The approval command is copyable, ready to run in the main repo.
    await expect(
      banner.locator(`[data-copy-command="cd ${beans.projectDir} && wt config approvals add"]`)
    ).toBeVisible();

    await expect(sidebar.locator('button[data-workspace-id="hooked"] [data-setup-failed]')).toHaveText(
      'Setup failed'
    );
    // Beans never approves hooks itself, so the hook did not run.
    expect(existsSync(marker)).toBe(false);

    // The banner can be dismissed.
    await banner.getByRole('button', { name: 'Dismiss setup error' }).click();
    await expect(banner).not.toBeVisible();
  });
});
