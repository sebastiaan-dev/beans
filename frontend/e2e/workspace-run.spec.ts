import { test, expect } from './fixtures';

test.describe('Workspace run experience', () => {
  test('Run button appears when run command is configured', async ({ beansWithRun, page }) => {
    await page.goto(beansWithRun.baseURL + '/');

    // Create a workspace
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    // Run button should be visible (identified by title "Run: <command>")
    await expect(page.getByTitle(/^Run:/)).toBeVisible({ timeout: 5_000 });
  });

  test('Run button is hidden when no run command is configured', async ({ beans, page }) => {
    await page.goto(beans.baseURL + '/');

    // Create a workspace
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    // Wait for workspace to load
    await expect(page.getByRole('button', { name: 'VS Code' })).toBeVisible({ timeout: 5_000 });

    // Run button should NOT be visible
    await expect(page.getByTitle(/^Run:/)).not.toBeVisible();
  });

  test('clicking Run starts the process and shows Stop button', async ({ beansWithRun, page }) => {
    await page.goto(beansWithRun.baseURL + '/');

    // Create a workspace
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    // Click the Run button
    const runButton = page.getByTitle(/^Run:/);
    await expect(runButton).toBeVisible({ timeout: 5_000 });
    await runButton.click();

    // Stop button should appear
    const stopButton = page.getByRole('button', { name: 'Stop' });
    await expect(stopButton).toBeVisible({ timeout: 5_000 });

    // Toolbar Run button should be gone (replaced by Stop)
    await expect(runButton).not.toBeVisible();

    // Run pane should be visible (only Run, not Terminal)
    await expect(page.getByRole('textbox', { name: 'Terminal input' })).toHaveCount(1, {
      timeout: 5_000
    });
  });

  test('clicking Stop kills the process and reverts to Run button', async ({
    beansWithRun,
    page
  }) => {
    await page.goto(beansWithRun.baseURL + '/');

    // Create a workspace and start run
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    const runButton = page.getByTitle(/^Run:/);
    await expect(runButton).toBeVisible({ timeout: 5_000 });
    await runButton.click();

    // Wait for Stop button
    const stopButton = page.getByRole('button', { name: 'Stop' });
    await expect(stopButton).toBeVisible({ timeout: 5_000 });

    // Click Stop
    await stopButton.click();

    // Run button should reappear
    await expect(page.getByTitle(/^Run:/)).toBeVisible({ timeout: 5_000 });

    // Stop button should be gone
    await expect(stopButton).not.toBeVisible();
  });

  test('Open button appears when running and has correct port', async ({ beansWithRun, page }) => {
    await page.goto(beansWithRun.baseURL + '/');

    // Create a workspace and start run
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });

    const runButton = page.getByTitle(/^Run:/);
    await expect(runButton).toBeVisible({ timeout: 5_000 });
    await runButton.click();

    // Open button should appear with a port URL
    const openButton = page.getByRole('button', { name: 'Open' });
    await expect(openButton).toBeVisible({ timeout: 5_000 });

    // Check it has a title with localhost and a port
    const title = await openButton.getAttribute('title');
    expect(title).toMatch(/http:\/\/localhost:\d+/);
  });
});

test.describe('Destroying a workspace with an active Run', () => {
  test('stops the Run process and terminal sessions before removing the worktree', async ({
    beansWithRun,
    page
  }) => {
    await page.goto(beansWithRun.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });
    const workspaceId = new URL(page.url()).pathname.split('/')[2];

    // Start the run process and open a shell terminal
    await page.getByTitle(/^Run:/).click();
    await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible({ timeout: 5_000 });
    await page.getByTitle('Show terminal').click();
    await expect(page.getByRole('textbox', { name: 'Terminal input' })).toHaveCount(2, {
      timeout: 5_000
    });

    const isRunning = async () =>
      (
        await beansWithRun.graphql<{ isRunning: boolean }>(
          `{ isRunning(workspaceId: "${workspaceId}") }`
        )
      ).isRunning;
    expect(await isRunning()).toBe(true);

    // Destroy the workspace from its toolbar; the dialog says the run will be stopped
    await page.getByRole('button', { name: 'Close Workspace' }).click();
    await expect(page.getByText('Its running process will be stopped.')).toBeVisible();
    await page.getByRole('button', { name: 'Destroy', exact: true }).click();
    await expect(page).toHaveURL(/\/planning/, { timeout: 5_000 });

    // The worktree disappears and its run process is no longer running
    await expect
      .poll(async () => (await beansWithRun.getWorktrees()).some((w) => w.id === workspaceId), {
        timeout: 10_000
      })
      .toBe(false);
    expect(await isRunning()).toBe(false);
  });
});

test.describe('Run command that exits on its own', () => {
  test.use({ runCommand: 'echo run-finished' });

  test('does not leave a bare shell behind in the Run pane', async ({ beansWithRun, page }) => {
    await page.goto(beansWithRun.baseURL + '/');
    await expect(page.getByText('Workspaces')).toBeVisible({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Create worktree' }).click();
    await expect(page).toHaveURL(/\/workspace\//, { timeout: 10_000 });
    const workspaceId = new URL(page.url()).pathname.split('/')[2];

    await page.getByTitle(/^Run:/).click();

    // Once the command finishes, the workspace goes back to the Run button and
    // the Run pane closes instead of showing an interactive prompt.
    await expect(page.getByTitle(/^Run:/)).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('textbox', { name: 'Terminal input' })).toHaveCount(0, {
      timeout: 5_000
    });

    // No shell was spawned under the run session ID on the server.
    await page.waitForTimeout(1_000);
    const { isRunning } = await beansWithRun.graphql<{ isRunning: boolean }>(
      `{ isRunning(workspaceId: "${workspaceId}") }`
    );
    expect(isRunning).toBe(false);
  });
});
