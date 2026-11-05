import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';

type Workspace = { id: string; name: string };

const MAIN: Workspace = { id: '__central__', name: 'main' };

/** Create a workspace via the sidebar and return its ID and sidebar label. */
async function createWorkspace(page: Page): Promise<Workspace> {
  const before = page.url();
  await page.getByRole('button', { name: 'Create worktree' }).click();
  await expect(page).not.toHaveURL(before, { timeout: 10_000 });
  await expect(page).toHaveURL(/\/workspace\/(?!__central__)[^/?]+/, { timeout: 10_000 });
  await expect(page.getByRole('button', { name: 'VS Code' })).toBeVisible({ timeout: 5_000 });
  const activeLabel = page.locator('nav button.font-medium span.truncate');
  await expect(activeLabel).toBeVisible({ timeout: 5_000 });
  return { id: new URL(page.url()).pathname.split('/')[2], name: (await activeLabel.textContent())! };
}

/** Navigate to a workspace through the sidebar (client-side navigation, no reload). */
async function switchTo(page: Page, ws: Workspace) {
  await page
    .locator('nav button')
    .filter({ has: page.locator('span.truncate', { hasText: new RegExp(`^${ws.name}$`) }) })
    .click();
  await expect(page).toHaveURL(new RegExp(`/workspace/${ws.id}`), { timeout: 5_000 });
  await expect(page.getByRole('button', { name: 'VS Code' })).toBeVisible({ timeout: 5_000 });
}

const terminalInputs = (page: Page) => page.getByRole('textbox', { name: 'Terminal input' });

test.describe('Terminal and Run panels are scoped per workspace', () => {
  test('opening the terminal in one workspace does not open it in another', async ({
    beans,
    page
  }) => {
    await page.goto(beans.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });

    const wsA = await createWorkspace(page);
    const wsB = await createWorkspace(page);

    // Open the terminal in B
    await page.getByTitle('Show terminal').click();
    await expect(terminalInputs(page)).toHaveCount(1, { timeout: 5_000 });
    await expect(page.getByTitle('Hide terminal')).toBeVisible();

    // A (and main) must not show a terminal
    await switchTo(page, wsA);
    await expect(page.getByTitle('Show terminal')).toBeVisible();
    await expect(terminalInputs(page)).toHaveCount(0);

    await switchTo(page, MAIN);
    await expect(page.getByTitle('Show terminal')).toBeVisible();
    await expect(terminalInputs(page)).toHaveCount(0);

    // B still remembers its open terminal
    await switchTo(page, wsB);
    await expect(page.getByTitle('Hide terminal')).toBeVisible();
    await expect(terminalInputs(page)).toHaveCount(1, { timeout: 5_000 });

    // Closing it in B keeps it closed when coming back
    await page.getByTitle('Hide terminal').click();
    await expect(terminalInputs(page)).toHaveCount(0);
    await switchTo(page, wsA);
    await switchTo(page, wsB);
    await expect(page.getByTitle('Show terminal')).toBeVisible();
    await expect(terminalInputs(page)).toHaveCount(0);
  });

  test('a Run started in one workspace only shows up in that workspace', async ({
    beansWithRun,
    page
  }) => {
    await page.goto(beansWithRun.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });

    const wsB = await createWorkspace(page);
    await page.getByTitle(/^Run:/).click();
    await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible({ timeout: 5_000 });
    await expect(terminalInputs(page)).toHaveCount(1, { timeout: 5_000 });

    // Main workspace: no Run panel, Run button (not Stop), no terminals at all
    await switchTo(page, MAIN);
    await expect(page.getByTitle(/^Run:/)).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Stop' })).toHaveCount(0);
    await expect(terminalInputs(page)).toHaveCount(0);

    // Back in B, the run is still active and its panel is shown
    await switchTo(page, wsB);
    await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Open', exact: true })).toBeVisible();
    await expect(terminalInputs(page)).toHaveCount(1, { timeout: 5_000 });

    // Stopping in B closes B's run panel
    await page.getByRole('button', { name: 'Stop' }).click();
    await expect(page.getByTitle(/^Run:/)).toBeVisible({ timeout: 5_000 });
    await expect(terminalInputs(page)).toHaveCount(0, { timeout: 5_000 });
  });

  test('switching away and back during a Run keeps the shell terminal per workspace', async ({
    beansWithRun,
    page
  }) => {
    await page.goto(beansWithRun.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });

    const wsA = await createWorkspace(page);
    const wsB = await createWorkspace(page);

    // B: run + terminal open => two terminals (shell + run)
    await page.getByTitle(/^Run:/).click();
    await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible({ timeout: 5_000 });
    await page.getByTitle('Show terminal').click();
    await expect(terminalInputs(page)).toHaveCount(2, { timeout: 5_000 });

    // A: nothing open
    await switchTo(page, wsA);
    await expect(terminalInputs(page)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Stop' })).toHaveCount(0);

    // B: both still there
    await switchTo(page, wsB);
    await expect(terminalInputs(page)).toHaveCount(2, { timeout: 5_000 });
  });
});
