import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { test, expect, findWorktrunk } from './fixtures';
import { scriptAgent, textTurn } from './agent-session';

/** CLI arguments of every agent process that received a user message. */
function receivedArgs(fakeClaudeDir: string): string[][] {
  const file = join(fakeClaudeDir, 'received.jsonl');
  if (!existsSync(file)) return [];
  return readFileSync(file, 'utf-8')
    .split('\n')
    .filter(Boolean)
    .map((line) => JSON.parse(line).args as string[]);
}

/** The parts of the beans fixture used here. */
interface Beans {
  baseURL: string;
  beansPath: string;
  fakeClaudeDir: string;
  createWorktree(name: string): Promise<{ id: string; path: string; branch: string }>;
  graphql(query: string): Promise<unknown>;
}

/**
 * Talks to a workspace's agent, destroys the workspace, recreates one with the
 * same name and checks that it starts with an empty chat and a new Claude
 * session. deleteBranch runs between destroying and recreating (the git
 * backend keeps the workspace branch, which would block reusing the name).
 */
async function checkSessionReset(beans: Beans, page: Page, deleteBranch: (branch: string) => void) {
  const name = 'reuse-me';
  const wt = await beans.createWorktree(name);
  scriptAgent(beans, [textTurn('Reply from the old workspace'), textTurn('Fresh reply')]);

  // Talk to the workspace's agent, so it has a conversation and a Claude
  // session ID to resume.
  await page.goto(beans.baseURL + `/workspace/${wt.id}`);
  await beans.graphql(
    `mutation { sendAgentMessage(beanId: ${JSON.stringify(wt.id)}, message: "Old task") }`
  );
  await expect(page.getByText('Reply from the old workspace')).toBeVisible({ timeout: 10_000 });
  const conversation = join(beans.beansPath, '.conversations', `${wt.id}.jsonl`);
  await expect.poll(() => existsSync(conversation)).toBe(true);

  // Destroy the workspace from the sidebar.
  const sidebar = page.locator('nav');
  const wsCard = sidebar.locator('div.rounded-md').filter({ hasText: name });
  await expect(wsCard).toBeVisible({ timeout: 5_000 });
  await wsCard.hover();
  await wsCard.getByRole('button', { name: 'Destroy worktree' }).click();
  await page.getByRole('button', { name: 'Destroy', exact: true }).click();
  await expect(sidebar.getByText(name)).not.toBeVisible({ timeout: 15_000 });
  // The persisted conversation (incl. the Claude session ID) is gone.
  await expect.poll(() => existsSync(conversation)).toBe(false);
  deleteBranch(wt.branch);

  // A new workspace with the same name gets a fresh conversation.
  const again = await beans.createWorktree(name);
  expect(again.id).toBe(wt.id);
  await page.goto(beans.baseURL + `/workspace/${again.id}`);
  await expect(page.getByText('Send a message to start a conversation')).toBeVisible({
    timeout: 10_000
  });
  await expect(page.getByText('Old task')).not.toBeVisible();
  await expect(page.getByText('Reply from the old workspace')).not.toBeVisible();

  // Its agent starts a new Claude session instead of resuming the old one.
  await beans.graphql(
    `mutation { sendAgentMessage(beanId: ${JSON.stringify(again.id)}, message: "New task") }`
  );
  await expect(page.getByText('Fresh reply')).toBeVisible({ timeout: 10_000 });
  const args = receivedArgs(beans.fakeClaudeDir);
  expect(args).toHaveLength(2);
  expect(args[0]).not.toContain('--resume');
  expect(args[1]).not.toContain('--resume');
  await expect(page.getByText('Old task')).not.toBeVisible();
}

test.describe('Workspace session reset', () => {
  test('a recreated workspace with the same name starts with an empty chat', async ({
    beans,
    page
  }) => {
    await checkSessionReset(beans, page, (branch) => {
      beans.git(beans.projectDir, ['branch', '-D', branch]);
    });
  });
});

test.describe('Workspace session reset with the worktrunk backend', () => {
  test.skip(!findWorktrunk(), 'worktrunk (wt) is not installed');
  test.use({ worktreeBackend: 'worktrunk' });

  test('a recreated workspace with the same name starts with an empty chat', async ({
    beans,
    page
  }) => {
    // wt remove deletes the (merged) branch itself.
    await checkSessionReset(beans, page, () => {});
  });
});
