<script lang="ts">
  import type {
    PendingInteraction,
    AskUserQuestionData,
    AskUserOption,
    QuestionAnswer
  } from '$lib/agentChat.svelte';
  import { untrack } from 'svelte';
  import { renderMarkdown } from '$lib/markdown';

  interface Props {
    interaction: PendingInteraction;
    onApprove: () => void;
    /** Submit answers to all pending questions; resolves to false on failure. */
    onAnswer: (answers: QuestionAnswer[]) => Promise<boolean>;
  }

  let { interaction, onApprove, onAnswer }: Props = $props();

  // Render plan content as markdown when available
  let renderedPlanContent = $state<string | null>(null);
  $effect(() => {
    const content = interaction.planContent;
    if (content) {
      renderMarkdown(content).then((html) => {
        renderedPlanContent = html;
      });
    } else {
      renderedPlanContent = null;
    }
  });

  // --- AskUserQuestion form -------------------------------------------------
  //
  // One AskUserQuestion call can carry several questions. Claude expects all
  // answers back together, so they're collected in a single form and submitted
  // at once. (Answering one at a time made the agent resume after the first
  // answer and re-ask the rest.) A lone single-select question keeps the
  // one-click shortcut.

  const questions = $derived(interaction.questions ?? []);
  const oneClick = $derived(questions.length === 1 && !questions[0].multiSelect);

  // Selected option labels and free-text "Other" answers, per question index.
  let selections = $state<string[][]>([]);
  let otherTexts = $state<string[]>([]);
  let submitting = $state(false);

  // Reset the form only when the questions themselves change. The session
  // subscription delivers a fresh interaction object on every update, so
  // keying on identity would wipe in-progress answers.
  const questionsKey = $derived(JSON.stringify(questions));
  let formKey: string | null = null;
  $effect(() => {
    if (questionsKey === formKey) return;
    formKey = questionsKey;
    untrack(() => {
      selections = questions.map(() => []);
      otherTexts = questions.map(() => '');
      submitting = false;
    });
  });

  function answersFor(index: number): string[] {
    const other = otherTexts[index]?.trim() ?? '';
    return [...(selections[index] ?? []), ...(other ? [other] : [])];
  }

  const answeredCount = $derived(questions.filter((_, i) => answersFor(i).length > 0).length);
  const allAnswered = $derived(questions.length > 0 && answeredCount === questions.length);

  function toggleOption(index: number, q: AskUserQuestionData, opt: AskUserOption) {
    if (submitting) return;
    if (oneClick) {
      submit([{ question: q.question, answers: [opt.label] }]);
      return;
    }
    const current = selections[index] ?? [];
    if (q.multiSelect) {
      selections[index] = current.includes(opt.label)
        ? current.filter((l) => l !== opt.label)
        : [...current, opt.label];
    } else {
      selections[index] = current.includes(opt.label) ? [] : [opt.label];
      otherTexts[index] = '';
    }
  }

  function setOtherText(index: number, q: AskUserQuestionData, text: string) {
    otherTexts[index] = text;
    // A single-select question takes either an option or a custom answer.
    if (!q.multiSelect && text.trim()) selections[index] = [];
  }

  function submitForm() {
    if (!allAnswered || submitting) return;
    submit(questions.map((q, i) => ({ question: q.question, answers: answersFor(i) })));
  }

  async function submit(answers: QuestionAnswer[]) {
    submitting = true;
    // On success the server clears the interaction and this form unmounts;
    // on failure, re-enable it so the user can retry.
    const ok = await onAnswer(answers);
    if (!ok) submitting = false;
  }
</script>

{#if interaction.type === 'EXIT_PLAN'}
  <div class="border-t border-status-in-progress-text/20 bg-status-in-progress-bg/50 p-3">
    <p class="mb-2 text-text-muted">Agent wants to leave plan mode and start working.</p>

    {#if renderedPlanContent}
      <div class="mb-3 max-h-96 overflow-y-auto rounded border border-border bg-surface p-3">
        <div class="agent-prose prose max-w-none min-w-0 text-text">
          {@html renderedPlanContent}
        </div>
      </div>
    {/if}

    <div class="flex items-center gap-3">
      <button
        onclick={onApprove}
        class="cursor-pointer rounded bg-status-in-progress-text px-3 py-1.5 text-white transition-colors hover:opacity-90"
      >
        Approve
      </button>
      <span class="text-text-muted">or type below to refine the plan</span>
    </div>
  </div>
{/if}

{#if interaction.type === 'ASK_USER'}
  <div class="border-t border-accent/30 bg-accent/5 px-4 py-3" data-testid="agent-questions">
    {#if questions.length}
      <form
        class="space-y-4"
        onsubmit={(e) => {
          e.preventDefault();
          submitForm();
        }}
      >
        {#each questions as q, i (i)}
          <fieldset class="space-y-2" disabled={submitting} data-testid="agent-question">
            <legend class="contents">
              {#if q.header}
                <span
                  class="inline-block rounded bg-accent/15 px-1.5 py-0.5 text-xs font-bold text-accent"
                >
                  {q.header}
                </span>
              {/if}
              <p class="mt-1 text-sm text-text">{q.question}</p>
              {#if q.multiSelect && !oneClick}
                <p class="text-xs text-text-faint">Select all that apply</p>
              {/if}
            </legend>
            <div class="flex flex-col gap-1.5">
              {#each q.options as opt, j (j)}
                {@const selected = selections[i]?.includes(opt.label) ?? false}
                <button
                  type="button"
                  aria-pressed={oneClick ? undefined : selected}
                  class={[
                    'cursor-pointer rounded border px-3 py-2 text-left text-xs transition-colors disabled:cursor-default disabled:opacity-60',
                    selected
                      ? 'border-accent bg-accent/15 text-accent'
                      : 'border-border hover:border-accent/50 hover:bg-accent/5'
                  ]}
                  onclick={() => toggleOption(i, q, opt)}
                >
                  <span class="font-bold text-text">{opt.label}</span>
                  {#if opt.description}
                    <span class="ml-2 text-text-muted">{opt.description}</span>
                  {/if}
                </button>
              {/each}
              {#if !oneClick}
                <input
                  type="text"
                  aria-label={`Other answer: ${q.question}`}
                  placeholder="Other…"
                  class="rounded border border-border bg-surface px-3 py-2 text-xs text-text placeholder:text-text-faint focus:border-accent focus:outline-none"
                  value={otherTexts[i] ?? ''}
                  oninput={(e) => setOtherText(i, q, e.currentTarget.value)}
                />
              {/if}
            </div>
          </fieldset>
        {/each}

        {#if !oneClick}
          <div class="flex items-center gap-3">
            <button
              type="submit"
              disabled={!allAnswered || submitting}
              class="cursor-pointer rounded bg-accent px-3 py-1.5 text-xs text-accent-text transition-colors hover:bg-accent/90 disabled:cursor-default disabled:opacity-50"
            >
              {submitting ? 'Sending…' : questions.length > 1 ? 'Submit answers' : 'Submit answer'}
            </button>
            {#if questions.length > 1}
              <span class="text-xs text-text-muted">
                {answeredCount} of {questions.length} answered
              </span>
            {/if}
          </div>
        {/if}
      </form>
      <p class="mt-3 text-xs text-text-faint">Or type a custom reply below.</p>
    {:else}
      <p class="text-xs text-accent">Agent is waiting for your answer — type your reply below.</p>
    {/if}
  </div>
{/if}
