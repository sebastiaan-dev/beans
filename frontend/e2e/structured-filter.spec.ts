import { test, expect } from './fixtures';
import type { Page } from '@playwright/test';
import type { BacklogPage } from './pages/backlog-page';

async function expectTitles(backlogPage: BacklogPage, want: string[]) {
  await expect(async () => {
    expect(await backlogPage.getBeanTitles()).toEqual(want);
  }).toPass({ timeout: 5_000 });
}

function filterInput(page: Page) {
  return page.getByTestId('filter-input');
}

function filterParam(page: Page): string | null {
  return new URL(page.url()).searchParams.get('q');
}

test.describe('Structured filter', () => {
  test('filters by status, type and priority syntax', async ({ beans, backlogPage, page }) => {
    beans.create('Urgent Bug', { status: 'todo', type: 'bug', priority: 'critical' });
    beans.create('Draft Bug', { status: 'draft', type: 'bug', priority: 'low' });
    beans.create('High Task', { status: 'todo', type: 'task', priority: 'high' });
    beans.create('Normal Task', { status: 'draft', type: 'task' });

    await backlogPage.goto(4);

    await filterInput(page).fill('type:bug');
    await expectTitles(backlogPage, ['Urgent Bug', 'Draft Bug']);

    await filterInput(page).fill('status:draft');
    await expectTitles(backlogPage, ['Normal Task', 'Draft Bug']);

    await filterInput(page).fill('priority:>=high');
    await expectTitles(backlogPage, ['Urgent Bug', 'High Task']);

    await filterInput(page).fill('-type:bug status:todo,draft');
    await expectTitles(backlogPage, ['High Task', 'Normal Task']);

    // Field terms combine with free text.
    await filterInput(page).fill('type:task normal');
    await expectTitles(backlogPage, ['Normal Task']);
  });

  test('filters by tag, ancestor and blocked state on the board', async ({
    beans,
    boardPage,
    page
  }) => {
    const milestone = beans.create('Version 0.2', { status: 'todo', type: 'milestone' });
    const feature = beans.create('Login Feature', { status: 'in-progress', type: 'feature' });
    const task = beans.create('Login Task', { status: 'todo', type: 'task' });
    beans.run(['update', feature, '--parent', milestone]);
    beans.run(['update', task, '--parent', feature, '--tag', 'ui']);
    const blocker = beans.create('Blocker', { status: 'todo', type: 'task' });
    const blocked = beans.create('Blocked Task', { status: 'todo', type: 'task' });
    beans.run(['update', blocked, '--blocked-by', blocker]);

    await boardPage.goto();
    await boardPage.waitForColumnCount('todo', 4);

    await filterInput(page).fill(`milestone:${milestone}`);
    await expect(async () => {
      expect(await boardPage.getColumnTitles('todo')).toEqual(['Login Task']);
      expect(await boardPage.getColumnTitles('in-progress')).toEqual(['Login Feature']);
    }).toPass({ timeout: 5_000 });

    // Ancestors can also be referenced by title.
    await filterInput(page).fill('milestone:"version 0.2" tag:ui');
    await expect(async () => {
      expect(await boardPage.getColumnTitles('todo')).toEqual(['Login Task']);
      expect(await boardPage.getColumnTitles('in-progress')).toEqual([]);
    }).toPass({ timeout: 5_000 });

    await filterInput(page).fill('blocked:yes');
    await expect(async () => {
      expect(await boardPage.getColumnTitles('todo')).toEqual(['Blocked Task']);
    }).toPass({ timeout: 5_000 });
  });

  test('autocompletes field names and values', async ({ beans, backlogPage, page }) => {
    beans.create('Draft One', { status: 'draft', type: 'task' });
    beans.create('Todo One', { status: 'todo', type: 'task' });

    await backlogPage.goto(2);

    const input = filterInput(page);
    await input.click();
    await input.pressSequentially('sta');
    const suggestions = page.getByTestId('filter-suggestion');
    await expect(suggestions.first()).toHaveText(/status:/);
    await input.press('Enter');
    await expect(input).toHaveValue('status:');

    await input.pressSequentially('dr');
    await expect(suggestions).toHaveCount(1);
    await expect(suggestions.first()).toContainText('draft');
    await suggestions.first().click();
    await expect(input).toHaveValue('status:draft ');
    await expectTitles(backlogPage, ['Draft One']);

    // Escape dismisses the suggestions.
    await input.pressSequentially('ty');
    await expect(suggestions.first()).toBeVisible();
    await input.press('Escape');
    await expect(suggestions).toHaveCount(0);
  });

  test('keeps the caret after text typed right after accepting a suggestion', async ({
    beans,
    backlogPage,
    page
  }) => {
    beans.create('Draft One', { status: 'draft', type: 'task' });
    await backlogPage.goto(1);

    const input = filterInput(page);
    await input.click();
    await input.pressSequentially('sta');
    await expect(page.getByTestId('filter-suggestion').first()).toHaveText(/status:/);

    // Accept with Enter and type "d" before the next frame, like a fast typist.
    const state = await input.evaluate(async (el: HTMLInputElement) => {
      el.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
      );
      // Let pending microtasks (Svelte's DOM updates) run, but no task.
      for (let i = 0; i < 20; i++) await Promise.resolve();
      el.setRangeText('d', el.selectionStart ?? 0, el.selectionEnd ?? 0, 'end');
      el.dispatchEvent(
        new InputEvent('input', { bubbles: true, data: 'd', inputType: 'insertText' })
      );
      await new Promise((resolve) => requestAnimationFrame(() => setTimeout(resolve)));
      return { value: el.value, caret: el.selectionStart };
    });
    expect(state).toEqual({ value: 'status:d', caret: 'status:d'.length });
  });

  test('popover checkboxes and chips edit the same query', async ({ beans, backlogPage, page }) => {
    beans.create('Some Bug', { status: 'todo', type: 'bug' });
    beans.create('Some Feature', { status: 'todo', type: 'feature' });
    beans.create('Some Task', { status: 'draft', type: 'task' });

    await backlogPage.goto(3);

    await filterInput(page).fill('some');
    await page.getByTestId('filter-popover-toggle').click();
    const popover = page.getByTestId('filter-popover');
    await expect(popover).toBeVisible();

    const typeSection = popover.locator('[data-filter-section="type"]');
    await typeSection.getByLabel('bug').check();
    await typeSection.getByLabel('feature').check();
    await expect(filterInput(page)).toHaveValue('some type:bug,feature');
    await expectTitles(backlogPage, ['Some Bug', 'Some Feature']);

    // Typed clauses are reflected in the (still open) popover.
    await filterInput(page).fill('status:draft');
    await expect(popover).toBeVisible();
    await expect(
      popover.locator('[data-filter-section="status"]').getByLabel('draft')
    ).toBeChecked();
    await expect(typeSection.getByLabel('bug')).not.toBeChecked();
    await popover
      .locator('[data-filter-section="blocked"]')
      .getByRole('button', { name: 'No' })
      .click();
    await expect(filterInput(page)).toHaveValue('status:draft blocked:no');
    await page.keyboard.press('Escape');
    await expect(popover).toHaveCount(0);

    // Active field filters are shown as removable chips.
    const chips = page.getByTestId('filter-chip');
    await expect(chips).toHaveCount(2);
    await expect(chips.first()).toContainText('status');
    await expect(chips.first()).toContainText('draft');
    await expect(page.getByTestId('filter-count')).toHaveText('2');

    await chips.first().getByTestId('filter-chip-remove').click();
    await expect(filterInput(page)).toHaveValue('blocked:no');
    await expectTitles(backlogPage, ['Some Bug', 'Some Feature', 'Some Task']);

    await filterInput(page).fill('status:nonsense');
    await expect(chips.first()).toHaveAttribute('title', /Unknown value/);
    await expect(page.getByText('No beans').first()).toBeVisible();

    // Plain text search has no chips.
    await filterInput(page).fill('bug');
    await expect(chips).toHaveCount(0);
  });

  test('persists the filter in the URL', async ({ beans, backlogPage, page }) => {
    beans.create('Draft Thing', { status: 'draft', type: 'task' });
    beans.create('Todo Thing', { status: 'todo', type: 'bug' });

    await backlogPage.goto(2);

    await filterInput(page).fill('status:draft thing');
    await expect.poll(() => filterParam(page)).toBe('status:draft thing');
    await expectTitles(backlogPage, ['Draft Thing']);

    // Restored on reload.
    await page.reload();
    await expect(filterInput(page)).toHaveValue('status:draft thing');
    await expectTitles(backlogPage, ['Draft Thing']);

    // Survives switching to the board.
    await page.getByRole('button', { name: 'Board' }).click();
    await page.waitForSelector('[data-status]', { timeout: 10_000 });
    await expect.poll(() => filterParam(page)).toBe('status:draft thing');

    // A shared link applies its filter, overriding the remembered one.
    await page.goto(`${beans.baseURL}/planning?q=${encodeURIComponent('type:bug')}`);
    await expect(filterInput(page)).toHaveValue('type:bug');
    await expectTitles(backlogPage, ['Todo Thing']);

    // Clearing removes the param.
    await page.getByTestId('filter-clear').click();
    await expect.poll(() => filterParam(page)).toBe(null);
    await expectTitles(backlogPage, ['Todo Thing', 'Draft Thing']);
  });

  test('shows non-matching ancestors as context in the backlog', async ({
    beans,
    backlogPage,
    page
  }) => {
    const epic = beans.create('Big Epic', { status: 'todo', type: 'epic' });
    const feature = beans.create('Mid Feature', { status: 'todo', type: 'feature' });
    const bug = beans.create('Deep Bug', { status: 'todo', type: 'bug' });
    beans.run(['update', feature, '--parent', epic]);
    beans.run(['update', bug, '--parent', feature]);
    beans.create('Other Task', { status: 'todo', type: 'task' });

    await backlogPage.goto(4);

    await filterInput(page).fill('type:bug');
    await expectTitles(backlogPage, ['Big Epic', 'Mid Feature', 'Deep Bug']);
    await expect(page.locator('.bean-item[data-filter-context]')).toHaveCount(2);
    await expect(page.locator(`.bean-item[data-bean-id="${bug}"]`)).not.toHaveAttribute(
      'data-filter-context'
    );
  });

  test('shows archived beans only when the query has an archived: clause', async ({
    beans,
    boardPage,
    backlogPage,
    page
  }) => {
    const oldDone = beans.create('Old Done', { status: 'completed', type: 'task' });
    const oldTodo = beans.create('Old Todo', { status: 'todo', type: 'task' });
    beans.create('Current Done', { status: 'completed', type: 'task' });
    beans.create('Current Todo', { status: 'todo', type: 'task' });

    await boardPage.goto();
    await boardPage.waitForColumnCount('todo', 2);
    await boardPage.waitForColumnCount('completed', 2);
    // Archive through the server (like the board's "archive all"); an archived
    // bean keeps its status, so the archived todo bean belongs to the backlog.
    for (const id of [oldDone, oldTodo]) {
      await beans.graphql(`mutation { archiveBean(id: "${id}") }`);
    }

    const columns = async () => ({
      todo: await boardPage.getColumnTitles('todo'),
      completed: await boardPage.getColumnTitles('completed')
    });
    const expectColumns = async (want: { todo: string[]; completed: string[] }) =>
      await expect(async () => expect(await columns()).toEqual(want)).toPass({ timeout: 5_000 });

    // Archived beans stay hidden by default and for filters without archived:.
    await expectColumns({ todo: ['Current Todo'], completed: ['Current Done'] });
    await filterInput(page).fill('done:yes');
    await expectColumns({ todo: [], completed: ['Current Done'] });

    await filterInput(page).fill('archived:yes');
    await expectColumns({ todo: ['Old Todo'], completed: ['Old Done'] });
    // "Archive all" only offers the beans that aren't archived yet.
    await expect(boardPage.archiveAllButton).not.toBeVisible();

    await filterInput(page).fill('archived:yes,no done:yes');
    await expectColumns({ todo: [], completed: ['Current Done', 'Old Done'] });
    await expect(boardPage.archiveAllButton).toBeVisible();

    await filterInput(page).fill('-archived:yes');
    await expectColumns({ todo: ['Current Todo'], completed: ['Current Done'] });

    // The backlog lists archived backlog beans as roots when asked for.
    await backlogPage.goto(1);
    await expectTitles(backlogPage, ['Current Todo']);
    await filterInput(page).fill('archived:yes');
    await expectTitles(backlogPage, ['Old Todo']);
    await filterInput(page).fill('archived:yes,no');
    await expectTitles(backlogPage, ['Current Todo', 'Old Todo']);
  });
});
