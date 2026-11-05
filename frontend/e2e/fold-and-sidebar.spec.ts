import { test, expect } from './fixtures';
import { SidebarPage } from './pages/sidebar-page';

test.describe('Folding beans with children', () => {
  test('folds and unfolds a parent, shows the child count, and persists across reload', async ({
    beans,
    backlogPage,
    page
  }) => {
    const milestone = beans.create('Milestone One', { status: 'todo', type: 'milestone' });
    const epic = beans.create('Epic Alpha', { status: 'todo', type: 'epic' });
    const task1 = beans.create('Task One', { status: 'todo', type: 'task' });
    const task2 = beans.create('Task Two', { status: 'todo', type: 'task' });
    beans.create('Loose Task', { status: 'todo', type: 'task' });
    beans.run(['update', epic, '--parent', milestone]);
    beans.run(['update', task1, '--parent', epic]);
    beans.run(['update', task2, '--parent', epic]);

    await backlogPage.goto(5);

    const toggle = backlogPage.foldToggle('Epic Alpha');
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // Beans without children have no toggle
    await expect(backlogPage.foldToggle('Loose Task')).toHaveCount(0);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toHaveAccessibleName('Expand Epic Alpha (2 children)');
    await expect(backlogPage.beanByTitle('Epic Alpha').locator('[data-child-count]')).toHaveText(
      '2'
    );
    await expect(page.getByText('Task One')).toHaveCount(0);
    await expect(page.getByText('Task Two')).toHaveCount(0);
    // The milestone itself stays expanded
    await expect(backlogPage.beanByTitle('Milestone One')).toContainText('Epic Alpha');

    // Persisted per project
    const { projectName } = await beans.graphql<{ projectName: string }>('{ projectName }');
    const stored = await page.evaluate(
      (key) => localStorage.getItem(key),
      `beans-folded:${projectName}`
    );
    expect(JSON.parse(stored!)).toEqual([epic]);

    await page.reload();
    await backlogPage.waitForBean('Milestone One');
    await expect(backlogPage.foldToggle('Epic Alpha')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByText('Task One')).toHaveCount(0);

    // Keyboard accessible: Enter on the focused toggle expands again
    await backlogPage.foldToggle('Epic Alpha').focus();
    await page.keyboard.press('Enter');
    await expect(backlogPage.foldToggle('Epic Alpha')).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByText('Task One')).toBeVisible();
    await expect(page.getByText('Task Two')).toBeVisible();

    // Folding doesn't select the bean
    await expect(page).not.toHaveURL(/bean=/);
  });

  test('collapse all and expand all', async ({ beans, backlogPage, page }) => {
    const milestone = beans.create('Milestone One', { status: 'todo', type: 'milestone' });
    const epic = beans.create('Epic Alpha', { status: 'todo', type: 'epic' });
    const feature = beans.create('Feature Draft', { status: 'draft', type: 'feature' });
    const task = beans.create('Task One', { status: 'todo', type: 'task' });
    const subtask = beans.create('Draft Child', { status: 'draft', type: 'task' });
    beans.run(['update', epic, '--parent', milestone]);
    beans.run(['update', task, '--parent', epic]);
    beans.run(['update', subtask, '--parent', feature]);

    await backlogPage.goto(5);

    await backlogPage.collapseAll();
    await expect(backlogPage.beanItems).toHaveCount(2);
    await expect(backlogPage.foldToggle('Milestone One')).toHaveAttribute('aria-expanded', 'false');
    await expect(backlogPage.foldToggle('Feature Draft')).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByRole('button', { name: 'Collapse all', exact: true })).toBeEnabled();

    // Expanding the milestone reveals the (still collapsed) epic
    await backlogPage.toggleFold('Milestone One');
    await expect(backlogPage.beanItems).toHaveCount(3);
    await expect(backlogPage.foldToggle('Epic Alpha')).toHaveAttribute('aria-expanded', 'false');

    await page.reload();
    await expect(backlogPage.beanItems).toHaveCount(3, { timeout: 10_000 });

    await backlogPage.expandAll();
    await expect(backlogPage.beanItems).toHaveCount(5);
    await expect(page.getByRole('button', { name: 'Expand all', exact: true })).toBeDisabled();

    await page.reload();
    await expect(backlogPage.beanItems).toHaveCount(5, { timeout: 10_000 });
  });

  test('dropping a bean onto a collapsed parent expands it', async ({ beans, backlogPage }) => {
    const parent = beans.create('Parent Feature', { status: 'todo', type: 'feature' });
    const child = beans.create('Existing Child', { status: 'todo', type: 'task' });
    beans.create('Mover', { status: 'todo', type: 'task' });
    beans.run(['update', child, '--parent', parent]);

    await backlogPage.goto(3);
    await backlogPage.toggleFold('Parent Feature');
    await expect(backlogPage.beanItems).toHaveCount(2);

    await backlogPage.dragBean('Mover', 'Parent Feature', 'onto');

    await expect(backlogPage.foldToggle('Parent Feature')).toHaveAttribute(
      'aria-expanded',
      'true',
      { timeout: 5_000 }
    );
    const parentItem = backlogPage.beanByTitle('Parent Feature');
    await expect(parentItem.locator('.bean-item', { hasText: 'Mover' })).toBeVisible();
    await expect(parentItem.locator('.bean-item', { hasText: 'Existing Child' })).toBeVisible();
  });

  test('reordering a collapsed parent keeps it collapsed', async ({ beans, backlogPage }) => {
    const parent = beans.create('Parent Feature', { status: 'todo', type: 'feature' });
    const child = beans.create('Hidden Child', { status: 'todo', type: 'task' });
    beans.create('Other Task', { status: 'todo', type: 'task' });
    beans.run(['update', child, '--parent', parent]);

    await backlogPage.goto(3);
    await backlogPage.toggleFold('Parent Feature');
    await expect(backlogPage.beanItems).toHaveCount(2);

    // Move whichever top-level bean is second to the top
    const before = await backlogPage.getBeanTitles();
    await backlogPage.dragBean(before[1], before[0], 'above');
    await expect(async () => {
      expect(await backlogPage.getBeanTitles()).toEqual([before[1], before[0]]);
    }).toPass({ timeout: 5_000 });

    await expect(backlogPage.foldToggle('Parent Feature')).toHaveAttribute(
      'aria-expanded',
      'false'
    );
    await expect(backlogPage.beanItems).toHaveCount(2);
  });

  test('a todo bean listed under a non-todo parent can be folded', async ({
    beans,
    backlogPage,
    page
  }) => {
    const parent = beans.create('Active Epic', { status: 'in-progress', type: 'epic' });
    const followUp = beans.create('Follow-up Feature', { status: 'todo', type: 'feature' });
    const grandchild = beans.create('Follow-up Task', { status: 'todo', type: 'task' });
    beans.run(['update', followUp, '--parent', parent]);
    beans.run(['update', grandchild, '--parent', followUp]);

    await backlogPage.goto(2);
    const item = backlogPage.beanByTitle('Follow-up Feature');
    await expect(item.locator('[data-parent-context]')).toContainText('Active Epic');

    await backlogPage.toggleFold('Follow-up Feature');
    await expect(backlogPage.beanItems).toHaveCount(1);
    await expect(item.locator('[data-parent-context]')).toContainText('Active Epic');
    await expect(page.getByText('Follow-up Task')).toHaveCount(0);

    await backlogPage.toggleFold('Follow-up Feature');
    await expect(page.getByText('Follow-up Task')).toBeVisible();
  });
});

test.describe('Collapsible sidebar', () => {
  test('collapses to an icon rail with the toggle button and shortcut, and persists', async ({
    beans,
    page
  }) => {
    const sidebar = new SidebarPage(page);
    await page.goto(beans.baseURL + '/');
    await sidebar.expectCollapsed(false);
    const expandedWidth = await sidebar.width();
    await expect(sidebar.nav.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });

    await sidebar.collapse();
    expect(await sidebar.width()).toBeLessThan(80);
    expect(expandedWidth).toBeGreaterThan(await sidebar.width());
    await expect(sidebar.nav.getByText('Workspaces')).toHaveCount(0);
    await expect(sidebar.expandButton).toHaveAttribute('aria-expanded', 'false');
    // Navigation stays available with tooltips
    await expect(sidebar.nav.getByRole('button', { name: 'Planning' })).toHaveAttribute(
      'title',
      'Planning'
    );

    await page.reload();
    await sidebar.expectCollapsed(true);

    // Keyboard shortcut toggles it (Cmd+\ on macOS, Ctrl+\ elsewhere)
    await page.locator('body').click();
    await page.keyboard.press('ControlOrMeta+Backslash');
    await sidebar.expectCollapsed(false);
    expect(Math.round(await sidebar.width())).toBe(Math.round(expandedWidth));

    await page.keyboard.press('ControlOrMeta+Backslash');
    await sidebar.expectCollapsed(true);

    await sidebar.expand();
    await page.reload();
    await sidebar.expectCollapsed(false);
  });

  test('workspaces can be switched while collapsed', async ({ beans, page }) => {
    const sidebar = new SidebarPage(page);
    await page.goto(beans.baseURL + '/');
    await expect(sidebar.nav.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });

    const wsId = await sidebar.createWorkspace();
    const { worktrees } = await beans.graphql<{
      worktrees: { id: string; name: string | null; branch: string }[];
    }>('{ worktrees { id name branch } }');
    const ws = worktrees.find((w) => w.id === wsId)!;
    const wsLabel = ws.name ?? ws.branch;

    await sidebar.nav.getByRole('button', { name: 'Planning' }).click();
    await expect(page).toHaveURL(/\/planning/);

    await sidebar.collapse();

    const railButton = sidebar.railWorkspace(wsLabel);
    await expect(railButton).toBeVisible();
    await expect(railButton).toHaveAttribute('title', new RegExp(`^${wsLabel}`));
    await railButton.click();
    await expect(page).toHaveURL(new RegExp(`/workspace/${wsId}`), { timeout: 5_000 });
    await expect(railButton).toHaveAttribute('aria-current', 'page');

    // Main workspace (labelled by the main branch) and planning are reachable too
    await sidebar.railWorkspace('main').click();
    await expect(page).toHaveURL(/\/workspace\/__central__/, { timeout: 5_000 });

    await sidebar.nav.getByRole('button', { name: 'Planning' }).click();
    await expect(page).toHaveURL(/\/planning/, { timeout: 5_000 });
    await sidebar.expectCollapsed(true);
  });
});
