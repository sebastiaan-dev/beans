import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect } from './fixtures';

/** PIDs of the fake claude agent processes started so far. */
function agentPids(fakeClaudeDir: string): number[] {
  const file = join(fakeClaudeDir, 'pids');
  if (!existsSync(file)) return [];
  return readFileSync(file, 'utf-8').split('\n').filter(Boolean).map(Number);
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

test.describe('Workspace destroy', () => {
  test('destroying a workspace stops its running agent', async ({ beans, page }) => {
    // A turn without events: the agent stays busy until it is stopped.
    writeFileSync(join(beans.fakeClaudeDir, 'script.json'), JSON.stringify([[]]));

    await page.goto(beans.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    const sidebar = page.locator('nav');
    const activeLabel = sidebar.locator('button.font-medium span.truncate');
    await expect(activeLabel).toBeVisible({ timeout: 5_000 });
    const wsName = (await activeLabel.textContent())!;
    const wt = (await beans.getWorktrees()).find((w) => w.branch.includes(wsName));
    expect(wt).toBeTruthy();

    // Open the destroy confirmation while the agent is idle (the sidebar hides
    // the destroy button while an agent runs), then the agent starts working
    // before the user confirms.
    const wsCard = sidebar.locator('div.rounded-md').filter({ hasText: wsName });
    await wsCard.hover();
    await wsCard.getByRole('button', { name: 'Destroy worktree' }).click();
    const confirm = page.getByRole('button', { name: 'Destroy', exact: true });
    await expect(confirm).toBeVisible({ timeout: 2_000 });

    await beans.graphql(
      `mutation { sendAgentMessage(beanId: ${JSON.stringify(wt!.id)}, message: "Work forever") }`
    );
    await expect.poll(() => agentPids(beans.fakeClaudeDir).length, { timeout: 10_000 }).toBe(1);
    const [pid] = agentPids(beans.fakeClaudeDir);
    expect(isAlive(pid)).toBe(true);

    await confirm.click();

    await expect(sidebar.getByText(wsName)).not.toBeVisible({ timeout: 15_000 });
    // The server stopped the agent before removing the worktree.
    expect(isAlive(pid)).toBe(false);
    expect(existsSync(wt!.path)).toBe(false);
  });

  test('createWorktree rejects names that are not valid branch names', async ({ beans }) => {
    const res = await fetch(`${beans.baseURL}/api/graphql`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: 'mutation { createWorktree(name: "Run Test") { id } }' })
    });
    const json = await res.json();
    expect(json.errors?.[0]?.message).toContain('invalid worktree name "Run Test"');
    expect(json.errors?.[0]?.message).toContain('"run-test"');
    expect(await beans.getWorktrees()).toHaveLength(0);
  });
});
