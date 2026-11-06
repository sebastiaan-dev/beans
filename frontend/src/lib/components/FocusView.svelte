<script lang="ts">
  import { beansStore, type Bean } from '$lib/beans.svelte';
  import { focusMode } from '$lib/focusMode.svelte';
  import { hasRelated, relatedBeans } from '$lib/focusMode';
  import { ui } from '$lib/uiState.svelte';
  import RenderedMarkdown from './RenderedMarkdown.svelte';

  const bean = $derived(focusMode.bean);
  const related = $derived(
    bean ? relatedBeans(bean, beansStore.all, (id) => beansStore.get(id)) : null
  );
  const position = $derived(focusMode.position);
  const sourceLabel = $derived(focusMode.source === 'board' ? 'Board' : 'Backlog');
  const hasList = $derived(focusMode.source !== 'none');

  let main = $state<HTMLElement | null>(null);
  let article = $state<HTMLElement | null>(null);

  // Focus the article (and start at the top) whenever another bean is shown.
  const shownId = $derived(bean?.id ?? null);
  $effect(() => {
    if (!shownId || !article) return;
    main?.scrollTo({ top: 0 });
    article.focus({ preventScroll: true });
  });

  // Return focus to the article when the edit form closes.
  let formWasOpen = false;
  $effect(() => {
    const open = ui.showForm;
    if (formWasOpen && !open) article?.focus({ preventScroll: true });
    formWasOpen = open;
  });

  // Remember where the bean is in the list (stepping keeps working if it leaves it).
  $effect(() => {
    void position;
    focusMode.rememberPosition();
  });

  /** Plain clicks navigate within focus mode; modified clicks keep the link's default (new tab, …). */
  function followLink(e: MouseEvent, target: Bean) {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    focusMode.show(target.id);
  }

  const positionLabel = $derived(
    position.index >= 0
      ? `${position.index + 1} of ${position.total} in ${sourceLabel}`
      : `Not in ${sourceLabel}`
  );
</script>

{#snippet beanLink(target: Bean)}
  <a
    href={focusMode.href(target.id)}
    onclick={(e) => followLink(e, target)}
    class="group inline-flex max-w-full items-baseline gap-2 rounded-sm text-text decoration-text-faint/50 underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-accent"
    data-focus-related={target.id}
  >
    <span class="truncate">{target.title}</span>
    <span class="shrink-0 font-mono text-xs text-text-faint">{target.id}</span>
    <span class="shrink-0 text-xs text-text-faint">{target.status}</span>
  </a>
{/snippet}

{#snippet relatedGroup(label: string, beans: Bean[])}
  {#if beans.length > 0}
    <dt class="pt-0.5 text-xs font-medium tracking-wide text-text-faint uppercase">{label}</dt>
    <dd class="min-w-0">
      <ul class="space-y-1">
        {#each beans as b (b.id)}
          <li class="min-w-0">{@render beanLink(b)}</li>
        {/each}
      </ul>
    </dd>
  {/if}
{/snippet}

<div class="fixed inset-0 z-40 flex flex-col bg-surface text-text" data-focus-view>
  <nav
    class="flex h-12 shrink-0 items-center gap-1 border-b border-border/60 px-3 text-sm text-text-muted"
    aria-label="Focus mode"
  >
    <button
      class="btn-focus-bar"
      onclick={() => focusMode.exit()}
      title="Leave focus mode (Esc)"
      aria-keyshortcuts="Escape"
    >
      <span class="icon-[uil--arrow-left] size-4" aria-hidden="true"></span>
      Back
    </button>

    <div class="flex flex-1 items-center justify-center gap-1">
      {#if hasList}
        <button
          class="btn-focus-bar"
          onclick={() => focusMode.step('prev')}
          disabled={!position.prevId}
          title="Previous bean (← or K)"
          aria-label="Previous bean"
          aria-keyshortcuts="ArrowLeft k"
        >
          <span class="icon-[uil--angle-left] size-5" aria-hidden="true"></span>
        </button>
        <span
          class="min-w-28 text-center text-xs text-text-faint tabular-nums"
          data-focus-position
          aria-live="polite">{positionLabel}</span
        >
        <button
          class="btn-focus-bar"
          onclick={() => focusMode.step('next')}
          disabled={!position.nextId}
          title="Next bean (→ or J)"
          aria-label="Next bean"
          aria-keyshortcuts="ArrowRight j"
        >
          <span class="icon-[uil--angle-right] size-5" aria-hidden="true"></span>
        </button>
      {/if}
    </div>

    <button
      class="btn-focus-bar"
      onclick={() => focusMode.toggleWidth()}
      aria-pressed={focusMode.wide}
      title="{focusMode.wide ? 'Narrow' : 'Wide'} reading column (W)"
      aria-label="Wide reading column"
      aria-keyshortcuts="w"
    >
      <span class="icon-[uil--arrows-h] size-4" aria-hidden="true"></span>
    </button>
    <button
      class="btn-focus-bar"
      onclick={() => bean && ui.openEditForm(bean)}
      disabled={!bean}
      title="Edit (E)"
      aria-keyshortcuts="e"
    >
      <span class="icon-[uil--edit] size-4" aria-hidden="true"></span>
      Edit
    </button>
  </nav>

  <main bind:this={main} class="min-h-0 flex-1 overflow-auto">
    {#if bean}
      <article
        bind:this={article}
        tabindex="-1"
        aria-labelledby="focus-title"
        class={[
          'focus-article mx-auto px-6 pt-12 pb-24 outline-none sm:pt-16',
          focusMode.wide ? 'max-w-[100ch]' : 'max-w-[72ch]'
        ]}
        data-focus-article={bean.id}
      >
        <header class="mb-10">
          <h1
            id="focus-title"
            class="text-[2rem] leading-tight font-semibold tracking-tight text-balance text-text"
          >
            {bean.title}
          </h1>
          <ul
            class="mt-4 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm text-text-muted"
            aria-label="Details"
            data-focus-meta
          >
            <li class="font-mono text-xs">{bean.id}</li>
            <li aria-hidden="true" class="text-text-faint">·</li>
            <li>{bean.status}</li>
            <li aria-hidden="true" class="text-text-faint">·</li>
            <li>{bean.type}</li>
            {#if bean.priority && bean.priority !== 'normal'}
              <li aria-hidden="true" class="text-text-faint">·</li>
              <li>{bean.priority} priority</li>
            {/if}
            {#if bean.tags.length > 0}
              <li aria-hidden="true" class="text-text-faint">·</li>
              <li class="flex flex-wrap gap-x-2">
                {#each bean.tags as tag (tag)}<span>#{tag}</span>{/each}
              </li>
            {/if}
            {#if related?.parent}
              {@const parent = related.parent}
              <li aria-hidden="true" class="text-text-faint">·</li>
              <li class="min-w-0">
                in
                <a
                  href={focusMode.href(parent.id)}
                  onclick={(e) => followLink(e, parent)}
                  class="text-text underline decoration-text-faint/50 underline-offset-4 hover:decoration-text-muted"
                  data-focus-parent>{parent.title}</a
                >
              </li>
            {/if}
          </ul>
        </header>

        {#if bean.body.trim()}
          <RenderedMarkdown
            content={bean.body}
            class="bean-body prose focus-prose max-w-none"
            onBeanLink={(b) => focusMode.show(b.id)}
          />
        {:else}
          <p class="text-text-faint italic">No description.</p>
        {/if}

        {#if related && hasRelated(related)}
          <section
            class="mt-16 border-t border-border/60 pt-6"
            aria-labelledby="focus-related-heading"
            data-focus-related-section
          >
            <h2
              id="focus-related-heading"
              class="mb-4 text-xs font-semibold tracking-wide text-text-faint uppercase"
            >
              Related
            </h2>
            <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-3 text-sm">
              {@render relatedGroup('Parent', related.parent ? [related.parent] : [])}
              {@render relatedGroup('Children', related.children)}
              {@render relatedGroup('Blocking', related.blocking)}
              {@render relatedGroup('Blocked by', related.blockedBy)}
            </dl>
          </section>
        {/if}
      </article>
    {:else}
      <p class="p-12 text-center text-sm text-text-faint">Loading…</p>
    {/if}
  </main>
</div>

<style>
  .btn-focus-bar {
    display: inline-flex;
    height: 2rem;
    min-width: 2rem;
    cursor: pointer;
    align-items: center;
    justify-content: center;
    gap: 0.375rem;
    border-radius: 0.375rem;
    padding: 0 0.5rem;
    color: var(--th-text-muted);
    transition:
      color 150ms,
      background-color 150ms;
  }

  .btn-focus-bar:hover:not(:disabled) {
    background-color: var(--th-surface-dim);
    color: var(--th-text);
  }

  .btn-focus-bar[aria-pressed='true'] {
    background-color: var(--th-surface-dim);
    color: var(--th-text);
  }

  .btn-focus-bar:disabled {
    cursor: default;
    opacity: 0.35;
  }

  .btn-focus-bar:focus-visible {
    outline: 2px solid var(--th-accent);
    outline-offset: 1px;
  }

  /* Reading typography: larger text, generous line height and spacing. */
  .focus-article :global(.focus-prose) {
    font-size: 1.0625rem;
    line-height: 1.75;
  }

  .focus-article :global(.focus-prose :is(p, ul, ol, blockquote, pre, table)) {
    margin-top: 1.1em;
    margin-bottom: 1.1em;
  }

  .focus-article :global(.focus-prose h1) {
    font-size: 1.5rem;
    margin-top: 2.25rem;
  }

  .focus-article :global(.focus-prose h2) {
    font-size: 1.3rem;
    margin-top: 2rem;
  }

  .focus-article :global(.focus-prose h3) {
    font-size: 1.125rem;
    margin-top: 1.75rem;
  }

  .focus-article :global(.focus-prose :is(h1, h2, h3, h4, h5, h6):first-child) {
    margin-top: 0;
  }

  .focus-article :global(.focus-prose pre.shiki) {
    font-size: 0.875rem;
  }
</style>
