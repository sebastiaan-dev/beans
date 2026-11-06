import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';

type ImageEntry = { id: string; media_type: string };
type MessageEntry = {
  role: 'user' | 'assistant' | 'tool' | 'info';
  content: string;
  images?: ImageEntry[];
  diff?: string;
};
type InteractionType = 'EXIT_PLAN' | 'ENTER_PLAN' | 'ASK_USER';

export interface AskUserQuestionConfig {
  header: string;
  question: string;
  multiSelect?: boolean;
  options: { label: string; description?: string }[];
}

interface PendingInteractionConfig {
  type: InteractionType;
  planContent?: string;
  /** Structured questions (ASK_USER only). */
  questions?: AskUserQuestionConfig[];
}

/** A stream-json event emitted by the fake claude CLI. */
type StreamEvent = Record<string, unknown>;

/** Wrap an Anthropic API streaming event the way Claude Code's stream-json does. */
function streamEvent(event: StreamEvent): StreamEvent {
  return { type: 'stream_event', event };
}

/**
 * Fake-agent turn in which Claude streams some text and then calls the
 * AskUserQuestion tool with the given questions (input arrives in chunks).
 */
export function askUserQuestionTurn(
  text: string,
  questions: AskUserQuestionConfig[]
): StreamEvent[] {
  const input = JSON.stringify({
    questions: questions.map((q) => ({
      header: q.header,
      question: q.question,
      multiSelect: q.multiSelect ?? false,
      options: q.options.map((o) => ({ label: o.label, description: o.description ?? '' }))
    }))
  });
  const mid = Math.floor(input.length / 2);
  return [
    streamEvent({ type: 'content_block_start', content_block: { type: 'text', text: '' } }),
    streamEvent({ type: 'content_block_delta', delta: { type: 'text_delta', text } }),
    streamEvent({ type: 'content_block_stop' }),
    streamEvent({
      type: 'content_block_start',
      content_block: { type: 'tool_use', name: 'AskUserQuestion' }
    }),
    streamEvent({
      type: 'content_block_delta',
      delta: { type: 'input_json_delta', partial_json: input.slice(0, mid) }
    }),
    streamEvent({
      type: 'content_block_delta',
      delta: { type: 'input_json_delta', partial_json: input.slice(mid) }
    }),
    streamEvent({ type: 'content_block_stop' })
  ];
}

/** Fake-agent turn that replies with plain text and ends the turn. */
export function textTurn(text: string): StreamEvent[] {
  return [
    streamEvent({ type: 'content_block_start', content_block: { type: 'text', text: '' } }),
    streamEvent({ type: 'content_block_delta', delta: { type: 'text_delta', text } }),
    streamEvent({ type: 'content_block_stop' }),
    { type: 'result', subtype: 'success', session_id: 'fake-session' }
  ];
}

/** Script the fake claude CLI: one array of stream events per user message. */
export function scriptAgent(beans: { fakeClaudeDir: string }, turns: StreamEvent[][]): void {
  writeFileSync(join(beans.fakeClaudeDir, 'script.json'), JSON.stringify(turns));
}

/** Text content of every user message the fake claude CLI has received. */
export function receivedAgentMessages(beans: { fakeClaudeDir: string }): string[] {
  const file = join(beans.fakeClaudeDir, 'received.jsonl');
  if (!existsSync(file)) return [];
  return readFileSync(file, 'utf-8')
    .split('\n')
    .filter(Boolean)
    .map((line) => {
      const content = JSON.parse(line).message.content;
      if (typeof content === 'string') return content;
      return content
        .filter((block: { type: string }) => block.type === 'text')
        .map((block: { text: string }) => block.text)
        .join('');
    });
}

/** Send a GraphQL query/mutation to the beans server. */
export async function gql(baseURL: string, query: string, variables?: Record<string, unknown>) {
  const res = await fetch(`${baseURL}/api/graphql`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, variables })
  });
  return res.json();
}

/**
 * Builder for setting up agent session state in e2e tests.
 *
 * Handles the details of JSONL file seeding (for messages) and GraphQL
 * mutations (for session state), in the correct order.
 *
 * Usage:
 *   await agentSession('__central__', beans)
 *     .withMessages([
 *       { role: 'user', content: 'Plan a refactor' },
 *       { role: 'assistant', content: 'Here is my plan.' }
 *     ])
 *     .inPlanMode()
 *     .withPendingInteraction({ type: 'EXIT_PLAN', planContent: '# Plan\n...' })
 *     .open(page);
 */
export function agentSession(beanId: string, beans: { beansPath: string; baseURL: string }) {
  return new AgentSessionBuilder(beanId, beans.beansPath, beans.baseURL);
}

class AgentSessionBuilder {
  private messages: MessageEntry[] = [];
  private planMode = false;
  private actMode = false;
  private pendingInteraction: PendingInteractionConfig | null = null;

  constructor(
    private beanId: string,
    private beansPath: string,
    private baseURL: string
  ) {}

  /** Seed conversation messages (persisted to JSONL, loaded on subscription connect). */
  withMessages(messages: MessageEntry[]): this {
    this.messages = messages;
    return this;
  }

  /** Set the session to plan mode. */
  inPlanMode(): this {
    this.planMode = true;
    this.actMode = false;
    return this;
  }

  /** Set the session to act mode. */
  inActMode(): this {
    this.actMode = true;
    this.planMode = false;
    return this;
  }

  /** Set a pending interaction (shown as approval UI in the chat). */
  withPendingInteraction(config: PendingInteractionConfig): this {
    this.pendingInteraction = config;
    return this;
  }

  /**
   * Apply the session state and open the chat panel.
   *
   * Order matters:
   * 1. Seed JSONL file (messages loaded lazily on subscription connect)
   * 2. Navigate and open chat (triggers subscription, loads messages)
   * 3. Apply mutations (pushed to frontend via subscription)
   */
  async open(page: Page): Promise<void> {
    // 1. Seed messages to JSONL
    if (this.messages.length > 0) {
      const convDir = join(this.beansPath, '.conversations');
      mkdirSync(convDir, { recursive: true });
      const lines = this.messages.map((m) => {
        const entry: Record<string, unknown> = {
          type: 'message',
          role: m.role,
          content: m.content
        };
        if (m.images && m.images.length > 0) entry.images = m.images;
        if (m.diff) entry.diff = m.diff;
        return JSON.stringify(entry);
      });
      writeFileSync(join(convDir, `${this.beanId}.jsonl`), lines.join('\n') + '\n');
    }

    // 2. Navigate to the main workspace (agent chat is always visible there)
    await page.goto(this.baseURL + `/workspace/${this.beanId}`);

    // Wait for messages to load if we seeded any
    if (this.messages.length > 0) {
      const firstMsg = this.messages[0].content;
      const { expect } = await import('@playwright/test');
      await expect(page.locator(`text=${firstMsg}`)).toBeVisible({ timeout: 5000 });
    }

    // 3. Apply session state via GraphQL mutations
    if (this.planMode) {
      await gql(
        this.baseURL,
        `mutation { setAgentPlanMode(beanId: "${this.beanId}", planMode: true) }`
      );
    }
    if (this.actMode) {
      await gql(
        this.baseURL,
        `mutation { setAgentActMode(beanId: "${this.beanId}", actMode: true) }`
      );
    }
    if (this.pendingInteraction) {
      const { type, planContent, questions } = this.pendingInteraction;
      const result = await gql(
        this.baseURL,
        `mutation($beanId: ID!, $planContent: String, $questions: [AskUserQuestionInput!]) {
          setAgentPendingInteraction(beanId: $beanId, type: ${type}, planContent: $planContent, questions: $questions)
        }`,
        {
          beanId: this.beanId,
          planContent: planContent ?? null,
          questions:
            questions?.map((q) => ({
              header: q.header,
              question: q.question,
              multiSelect: q.multiSelect ?? false,
              options: q.options.map((o) => ({ label: o.label, description: o.description ?? '' }))
            })) ?? null
        }
      );
      if (result.errors) throw new Error(JSON.stringify(result.errors));
    }
  }
}
