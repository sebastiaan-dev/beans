import { test, expect } from './fixtures';
import {
  agentSession,
  askUserQuestionTurn,
  gql,
  receivedAgentMessages,
  scriptAgent,
  textTurn,
  type AskUserQuestionConfig
} from './agent-session';

const questions: AskUserQuestionConfig[] = [
  {
    header: 'Database',
    question: 'Which database should we use?',
    options: [
      { label: 'PostgreSQL', description: 'Relational, battle-tested' },
      { label: 'SQLite', description: 'Embedded, zero setup' }
    ]
  },
  {
    header: 'Language',
    question: 'Which language should the service use?',
    options: [{ label: 'Go' }, { label: 'Rust' }]
  },
  {
    header: 'Features',
    question: 'Which features should be included?',
    multiSelect: true,
    options: [{ label: 'Auth' }, { label: 'Billing' }, { label: 'Search' }]
  }
];

const expectedAnswers =
  'Here are my answers to your questions:\n' +
  '\n- "Which database should we use?"="SQLite"' +
  '\n- "Which language should the service use?"="Rust"' +
  '\n- "Which features should be included?"="Auth", "Search"';

test.describe('Agent questions (AskUserQuestion)', () => {
  test('multiple questions are answered in one form and sent to the agent together', async ({
    page,
    beans
  }) => {
    scriptAgent(beans, [
      askUserQuestionTurn('I have a few questions first.', questions),
      textTurn('Thanks, setting things up now.')
    ]);

    await page.goto(`${beans.baseURL}/workspace/__central__`);
    await gql(
      beans.baseURL,
      'mutation($m: String!) { sendAgentMessage(beanId: "__central__", message: $m) }',
      { m: 'Set up the project' }
    );

    const form = page.getByTestId('agent-questions');
    const questionGroups = form.getByTestId('agent-question');
    await expect(questionGroups).toHaveCount(3, { timeout: 10_000 });
    const submit = form.getByRole('button', { name: 'Submit answers' });
    await expect(submit).toBeDisabled();
    await expect(form.getByText('0 of 3 answered')).toBeVisible();

    // Answering one question must not send anything or hide the others.
    await questionGroups
      .nth(0)
      .getByRole('button', { name: /SQLite/ })
      .click();
    await expect(questionGroups).toHaveCount(3);
    await expect(questionGroups.nth(0).getByRole('button', { name: /SQLite/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    );
    await expect(form.getByText('1 of 3 answered')).toBeVisible();
    await expect(submit).toBeDisabled();

    // Switching a single-select answer replaces the previous choice.
    await questionGroups.nth(1).getByRole('button', { name: 'Go' }).click();
    await questionGroups.nth(1).getByRole('button', { name: 'Rust' }).click();
    await expect(questionGroups.nth(1).getByRole('button', { name: 'Go' })).toHaveAttribute(
      'aria-pressed',
      'false'
    );

    // Multi-select toggles options independently.
    const features = questionGroups.nth(2);
    await features.getByRole('button', { name: 'Auth' }).click();
    await features.getByRole('button', { name: 'Billing' }).click();
    await features.getByRole('button', { name: 'Search' }).click();
    await features.getByRole('button', { name: 'Billing' }).click();
    await expect(form.getByText('3 of 3 answered')).toBeVisible();

    // Nothing has been sent to the agent beyond the initial prompt yet.
    expect(receivedAgentMessages(beans)).toHaveLength(1);

    await submit.click();

    // The agent receives every answer in a single message.
    await expect.poll(() => receivedAgentMessages(beans).length, { timeout: 10_000 }).toBe(2);
    expect(receivedAgentMessages(beans)[1]).toContain(expectedAnswers);
    await expect(page.getByText('Thanks, setting things up now.')).toBeVisible({ timeout: 10_000 });

    // The form is gone and the agent didn't ask again.
    await expect(form).not.toBeVisible();
    await page.waitForTimeout(500);
    await expect(form).not.toBeVisible();
    expect(receivedAgentMessages(beans)).toHaveLength(2);
  });

  test('a single single-select question is answered with one click', async ({ page, beans }) => {
    scriptAgent(beans, [
      askUserQuestionTurn('Quick question.', [questions[0]]),
      textTurn('Going with that.')
    ]);

    await page.goto(`${beans.baseURL}/workspace/__central__`);
    await gql(
      beans.baseURL,
      'mutation($m: String!) { sendAgentMessage(beanId: "__central__", message: $m) }',
      { m: 'Pick a database' }
    );

    const form = page.getByTestId('agent-questions');
    await expect(form.getByTestId('agent-question')).toHaveCount(1, { timeout: 10_000 });
    await expect(form.getByRole('button', { name: /Submit/ })).toHaveCount(0);

    await form.getByRole('button', { name: /PostgreSQL/ }).click();

    await expect.poll(() => receivedAgentMessages(beans).length, { timeout: 10_000 }).toBe(2);
    expect(receivedAgentMessages(beans)[1]).toContain(
      'Here are my answers to your questions:\n\n- "Which database should we use?"="PostgreSQL"'
    );
    await expect(page.getByText('Going with that.')).toBeVisible({ timeout: 10_000 });
    await expect(form).not.toBeVisible();
  });

  test('in-progress answers survive unrelated session updates and accept free text', async ({
    page,
    beans
  }) => {
    await agentSession('__central__', beans)
      .withMessages([{ role: 'user', content: 'Set up the project' }])
      .withPendingInteraction({ type: 'ASK_USER', questions: questions.slice(0, 2) })
      .open(page);

    const form = page.getByTestId('agent-questions');
    const questionGroups = form.getByTestId('agent-question');
    await expect(questionGroups).toHaveCount(2, { timeout: 5000 });

    await questionGroups
      .nth(0)
      .getByRole('button', { name: /SQLite/ })
      .click();
    await questionGroups
      .nth(1)
      .getByRole('textbox', { name: /Other answer/ })
      .fill('Zig, if possible');

    // An unrelated session change pushes a fresh session snapshot.
    await gql(beans.baseURL, 'mutation { setAgentEffort(beanId: "__central__", effort: "high") }');
    await expect(page.getByRole('button', { name: 'High', exact: true })).toHaveClass(
      /text-accent/
    );

    await expect(questionGroups.nth(0).getByRole('button', { name: /SQLite/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    );
    await expect(questionGroups.nth(1).getByRole('textbox', { name: /Other answer/ })).toHaveValue(
      'Zig, if possible'
    );

    await form.getByRole('button', { name: 'Submit answers' }).click();

    await expect.poll(() => receivedAgentMessages(beans).length, { timeout: 10_000 }).toBe(1);
    expect(receivedAgentMessages(beans)[0]).toContain(
      'Here are my answers to your questions:\n' +
        '\n- "Which database should we use?"="SQLite"' +
        '\n- "Which language should the service use?"="Zig, if possible"'
    );
    await expect(form).not.toBeVisible({ timeout: 5000 });
  });
});
