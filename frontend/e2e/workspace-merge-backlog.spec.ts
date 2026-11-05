import type { Locator, Page } from '@playwright/test';
import { test, expect } from './fixtures';

/** Card title spans with exactly `title`, within `scope`. */
function cardTitle(scope: Locator | Page, title: string): Locator {
  const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return scope.locator('[role="button"] > div > span.text-sm', {
    hasText: new RegExp(`^${escaped}$`)
  });
}

function section(page: Page, status: string): Locator {
  return page.locator(`[data-backlog-section="${status}"]`);
}

/**
 * Regression tests for hmans/beans#181: beans created by an agent in a
 * workspace and merged back into main must show up in the backlog view,
 * not just on the board.
 *
 * Agents typically mark the bean they work on as in-progress/completed and
 * file follow-up work as children of it. The backlog used to render only
 * top-level todo/draft beans (plus their subtrees), so such follow-ups were
 * unreachable there.
 */
test.describe('Workspace beans merged into main', () => {
  test('follow-up beans created under the worked-on bean appear in the backlog', async ({
    beans,
    backlogPage,
    boardPage,
    page
  }) => {
    const featureId = beans.create('Worked Feature', { type: 'feature', status: 'todo' });
    beans.git(beans.projectDir, ['add', '.beans', '.beans.yml']);
    beans.git(beans.projectDir, ['commit', '-m', 'add beans']);

    const wt = await beans.createWorktree('merge-backlog');

    // Simulate the agent: work on the feature, file follow-ups, then complete it.
    beans.runInWorktree(wt.path, ['update', featureId, '-s', 'in-progress']);
    beans.runInWorktree(wt.path, [
      'create',
      'Follow-up Todo',
      '-t',
      'task',
      '-s',
      'todo',
      '--parent',
      featureId
    ]);
    beans.runInWorktree(wt.path, [
      'create',
      'Follow-up Draft',
      '-t',
      'task',
      '-s',
      'draft',
      '--parent',
      featureId
    ]);
    const topLevelId = JSON.parse(
      beans.runInWorktree(wt.path, [
        'create',
        '--json',
        'Top-level Follow-up',
        '-t',
        'task',
        '-s',
        'todo'
      ])
    ).bean.id as string;
    beans.runInWorktree(wt.path, ['update', featureId, '-s', 'completed']);
    beans.git(wt.path, ['add', '.beans']);
    beans.git(wt.path, ['commit', '-m', 'agent work']);

    // Integrate the branch into main (fast-forward, like the local integrate flow).
    beans.git(beans.projectDir, ['merge', '--ff-only', wt.branch]);

    // Board: follow-ups are in the Todo column (this always worked).
    await boardPage.goto();
    await expect(async () => {
      const todo = await boardPage.getColumnTitles('todo');
      expect(todo).toEqual(expect.arrayContaining(['Follow-up Todo', 'Top-level Follow-up']));
    }).toPass({ timeout: 10_000 });

    // Backlog: follow-ups must be visible in their status sections.
    await backlogPage.goto();
    const todoSection = section(page, 'todo');
    const draftSection = section(page, 'draft');
    await expect(cardTitle(todoSection, 'Follow-up Todo')).toBeVisible({
      timeout: 10_000
    });
    await expect(cardTitle(todoSection, 'Top-level Follow-up')).toBeVisible();
    await expect(cardTitle(draftSection, 'Follow-up Draft')).toBeVisible();

    // The completed parent itself is not part of the backlog.
    await expect(cardTitle(page, 'Worked Feature')).toHaveCount(0);

    // Once merged, the beans are no longer tied to the workspace: edits made
    // in the UI go to the main working copy, not into the worktree.
    await expect(async () => {
      const data = await beans.graphql<{ bean: { worktreeId: string | null; isDirty: boolean } }>(
        `{ bean(id: "${topLevelId}") { worktreeId isDirty } }`
      );
      expect(data.bean).toEqual({ worktreeId: null, isDirty: false });
    }).toPass({ timeout: 10_000 });
    await beans.graphql(
      `mutation { updateBean(id: "${topLevelId}", input: { body: "edited after merge" }) { id } }`
    );
    expect(beans.git(beans.projectDir, ['status', '--porcelain', '.beans'])).toContain(topLevelId);
    expect(beans.git(wt.path, ['status', '--porcelain', '.beans'])).toBe('');
  });

  test('todo children of an in-progress parent are listed in the backlog', async ({
    beans,
    backlogPage,
    page
  }) => {
    const parentId = beans.create('Active Feature', { type: 'feature', status: 'in-progress' });
    beans.run(['create', 'Pending Subtask', '-t', 'task', '-s', 'todo', '--parent', parentId]);
    beans.run(['create', 'Done Subtask', '-t', 'task', '-s', 'completed', '--parent', parentId]);
    beans.create('Plain Todo', { type: 'task', status: 'todo' });

    await backlogPage.goto();
    const todoSection = section(page, 'todo');
    await expect(cardTitle(todoSection, 'Pending Subtask')).toBeVisible({
      timeout: 10_000
    });
    await expect(cardTitle(todoSection, 'Plain Todo')).toBeVisible();
    // The detached root shows which (non-backlog) parent it belongs to.
    const pending = todoSection.locator('.bean-item', { has: cardTitle(page, 'Pending Subtask') });
    await expect(pending.locator('[data-parent-context]')).toContainText('Active Feature');

    // Completed beans under a non-backlog parent stay hidden.
    await expect(cardTitle(page, 'Done Subtask')).toHaveCount(0);
    await expect(cardTitle(page, 'Active Feature')).toHaveCount(0);
  });

  test('reordering a detached root keeps its parent', async ({ beans, backlogPage }) => {
    const parentId = beans.create('Running Feature', { type: 'feature', status: 'in-progress' });
    const childId = JSON.parse(
      beans.run([
        'create',
        '--json',
        'Detached Task',
        '-t',
        'task',
        '-s',
        'todo',
        '--parent',
        parentId
      ])
    ).bean.id as string;
    beans.create('Another Todo', { type: 'task', status: 'todo', priority: 'high' });

    await backlogPage.goto(2);
    expect(await backlogPage.getBeanTitles()).toEqual(['Another Todo', 'Detached Task']);

    await backlogPage.dragBean('Detached Task', 'Another Todo', 'above');
    await expect(async () => {
      expect(await backlogPage.getBeanTitles()).toEqual(['Detached Task', 'Another Todo']);
    }).toPass({ timeout: 5_000 });

    const data = await beans.graphql<{ bean: { parentId: string | null; order: string } }>(
      `{ bean(id: "${childId}") { parentId order } }`
    );
    expect(data.bean.parentId).toBe(parentId);
    expect(data.bean.order).not.toBe('');
  });

  test('todo children of a todo parent stay nested (not duplicated)', async ({
    beans,
    backlogPage
  }) => {
    const parentId = beans.create('Todo Epic', { type: 'epic', status: 'todo' });
    beans.run(['create', 'Nested Child', '-t', 'task', '-s', 'todo', '--parent', parentId]);

    await backlogPage.goto(2);
    const nested = backlogPage.beanByTitle('Todo Epic').locator('.bean-item', {
      hasText: 'Nested Child'
    });
    await expect(nested).toHaveCount(1);
  });
});
