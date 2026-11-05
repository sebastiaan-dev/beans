import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect } from './fixtures';

/**
 * Regression test for hmans/beans#176: Start Work creates the worktree from the
 * base ref, which doesn't contain uncommitted bean edits made in Beans UI. The
 * server must copy the bean as it sees it into the new worktree, so the agent
 * doesn't start from a stale version.
 */
test.describe('Start Work bean sync', () => {
  // Start Work sends a prompt to the agent; never spawn a real one.

  test('the new workspace gets the bean including uncommitted edits', async ({
    beans,
    backlogPage,
    page
  }) => {
    const beanId = beans.create('Start Work Target', { type: 'task', status: 'todo' });
    const untouchedId = beans.create('Unrelated Bean', { type: 'task', status: 'todo' });
    beans.git(beans.projectDir, ['add', '.beans', '.beans.yml']);
    beans.git(beans.projectDir, ['commit', '-m', 'add beans']);

    // Edit the descriptions through the API used by the UI. The changes are
    // written to the main working copy but not committed.
    const edited = 'Refined requirements written just before Start Work.';
    const updateBody = async (id: string, body: string) => {
      // The server picks up CLI-created beans via its file watcher; retry
      // until it knows the bean.
      await expect(async () => {
        const data = await beans.graphql<{ updateBean: { id: string } | null }>(
          `mutation { updateBean(id: "${id}", input: { body: ${JSON.stringify(body)} }) { id } }`
        );
        expect(data?.updateBean?.id).toBe(id);
      }).toPass({ timeout: 10_000 });
    };
    await updateBody(beanId, edited);
    await updateBody(untouchedId, 'unrelated uncommitted edit');
    expect(beans.git(beans.projectDir, ['status', '--porcelain', '.beans'])).toContain(beanId);

    await backlogPage.goto();
    await backlogPage.selectBean('Start Work Target');
    const detail = page.locator('h1', { hasText: 'Start Work Target' }).locator('..');
    await detail.getByRole('button', { name: 'Start Work' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    const worktrees = await beans.getWorktrees();
    const wtId = page.url().split('/workspace/')[1].split(/[?#]/)[0];
    const wt = worktrees.find((w) => w.id === decodeURIComponent(wtId));
    expect(wt).toBeTruthy();

    const wtBeansDir = join(wt!.path, '.beans');
    const file = readdirSync(wtBeansDir).find((f) => f.startsWith(beanId));
    expect(file).toBeTruthy();
    expect(readFileSync(join(wtBeansDir, file!), 'utf-8')).toContain(edited);

    // Only the bean being worked on is copied; it is a change on the branch.
    const wtStatus = beans.git(wt!.path, ['status', '--porcelain', '.beans']);
    expect(wtStatus).toContain(beanId);
    expect(wtStatus).not.toContain(untouchedId);
  });
});
