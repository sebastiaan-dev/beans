import { test, expect } from './fixtures';
import { agentSession } from './agent-session';

const planMessages = [
  { role: 'user' as const, content: 'Plan a refactor' },
  { role: 'assistant' as const, content: 'Here is my plan for the refactor.' }
];

test.describe('Plan mode approval flow', () => {
  test('ExitPlanMode shows approval UI with plan content and hint text', async ({
    page,
    beans
  }) => {
    await agentSession('__central__', beans)
      .withMessages(planMessages)
      .inPlanMode()
      .withPendingInteraction({
        type: 'EXIT_PLAN',
        planContent: '# Refactor Plan\n\n1. Extract module\n2. Update imports'
      })
      .open(page);

    // Verify the approval UI is visible
    await expect(
      page.locator('text=Agent wants to leave plan mode and start working.')
    ).toBeVisible({ timeout: 5000 });

    // Verify plan content is rendered
    await expect(page.locator('text=Refactor Plan')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=Extract module')).toBeVisible();

    // Verify the Approve button is present
    await expect(page.locator('button:has-text("Approve")')).toBeVisible();

    // Verify the hint text is shown
    await expect(page.locator('text=or type below to refine the plan')).toBeVisible();

    // Verify there is NO Reject button
    await expect(page.locator('button:has-text("Reject")')).not.toBeVisible();
  });

  test('ENTER_PLAN interaction type does not show approval UI', async ({ page, beans }) => {
    await agentSession('__central__', beans)
      .withMessages(planMessages)
      .withPendingInteraction({ type: 'ENTER_PLAN' })
      .open(page);

    // Give the subscription time to push the update
    await page.waitForTimeout(500);

    // The ENTER_PLAN type should NOT show the ExitPlanMode approval UI
    await expect(
      page.locator('text=Agent wants to leave plan mode and start working.')
    ).not.toBeVisible();
  });

  test('Plan/Act mode toggle reflects session state', async ({ page, beans }) => {
    await agentSession('__central__', beans).withMessages(planMessages).inPlanMode().open(page);

    // The mode toggle should show "Plan" is active
    const planButton = page.getByRole('button', { name: 'Plan', exact: true });
    await expect(planButton).toBeVisible({ timeout: 5000 });
  });
});
