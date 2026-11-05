<script lang="ts">
  import { flushSync } from 'svelte';
  import { ui } from '$lib/uiState.svelte';
  import { activeClauses, suggest, type Suggestion } from '$lib/filter';
  import { planningFilter } from '$lib/planningFilter.svelte';
  import FilterPopover from './FilterPopover.svelte';

  let inputEl = $state<HTMLInputElement | null>(null);
  let rootEl = $state<HTMLDivElement | null>(null);
  const isMac = navigator.platform.startsWith('Mac');
  const shortcutHint = isMac ? '⌘/' : 'Ctrl+/';

  // Autocomplete
  let caret = $state(0);
  let focused = $state(false);
  let suggestionsDismissed = $state(false);
  let activeIndex = $state(0);
  const suggestions: Suggestion[] = $derived(
    focused && !suggestionsDismissed ? suggest(ui.filterText, caret, planningFilter.sources) : []
  );

  // Popover
  let popoverOpen = $state(false);
  const clauseCount = $derived(activeClauses(planningFilter.query).length);

  export function focus() {
    inputEl?.focus();
  }

  function updateCaret() {
    caret = inputEl?.selectionStart ?? ui.filterText.length;
  }

  function handleInput(e: Event & { currentTarget: HTMLInputElement }) {
    ui.setFilterText(e.currentTarget.value);
    suggestionsDismissed = false;
    activeIndex = 0;
    updateCaret();
  }

  function accept(s: Suggestion) {
    const text = ui.filterText;
    // Update the input and place the caret synchronously: deferring this (e.g.
    // `await tick()`, which waits for a frame in async mode) would move the
    // caret back over characters typed in the meantime.
    flushSync(() => {
      ui.setFilterText(text.slice(0, s.start) + s.insert + text.slice(s.end));
      activeIndex = 0;
    });
    const pos = s.start + s.insert.length;
    inputEl?.focus();
    inputEl?.setSelectionRange(pos, pos);
    caret = pos;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (suggestions.length > 0) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const delta = e.key === 'ArrowDown' ? 1 : -1;
        activeIndex = (activeIndex + delta + suggestions.length) % suggestions.length;
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        accept(suggestions[Math.min(activeIndex, suggestions.length - 1)]);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        suggestionsDismissed = true;
        return;
      }
    }
    if (e.key === 'Escape' && popoverOpen) {
      e.preventDefault();
      e.stopPropagation();
      popoverOpen = false;
    }
  }

  function handleWindowPointerDown(e: PointerEvent) {
    if (popoverOpen && rootEl && !rootEl.contains(e.target as Node)) popoverOpen = false;
  }
</script>

<svelte:window onpointerdown={handleWindowPointerDown} />

<div class="relative flex items-center gap-1" bind:this={rootEl}>
  <div class="relative min-w-0 flex-1">
    <input
      bind:this={inputEl}
      type="text"
      placeholder="Filter beans… ({shortcutHint})"
      value={ui.filterText}
      oninput={handleInput}
      onkeydown={handleKeydown}
      onkeyup={updateCaret}
      onclick={updateCaret}
      onfocus={() => {
        focused = true;
        suggestionsDismissed = false;
        updateCaret();
      }}
      onblur={() => (focused = false)}
      class="w-full rounded-md border-none bg-surface px-3 py-1.5 pr-8 text-sm text-text placeholder:text-text-faint focus:outline-none"
      title="Text search, or fields like status:todo,draft priority:>=high tag:ui milestone:<id> blocked:no"
      autocomplete="off"
      spellcheck="false"
      role="combobox"
      aria-expanded={suggestions.length > 0}
      aria-controls="filter-suggestions"
      aria-autocomplete="list"
      data-testid="filter-input"
    />
    {#if ui.filterText}
      <button
        onclick={() => {
          ui.setFilterText('');
          inputEl?.focus();
        }}
        class="absolute top-1/2 right-2 -translate-y-1/2 cursor-pointer text-text-muted hover:text-text"
        title="Clear filter"
        data-testid="filter-clear"
      >
        &#x2715;
      </button>
    {/if}

    {#if suggestions.length > 0}
      <ul
        id="filter-suggestions"
        role="listbox"
        class="absolute top-full left-0 z-[60] mt-1 max-h-64 w-full min-w-64 overflow-y-auto rounded-md border border-border bg-surface py-1 shadow-lg"
        data-testid="filter-suggestions"
      >
        {#each suggestions as s, i (s.label + s.detail)}
          <li
            role="option"
            aria-selected={i === activeIndex}
            class={[
              'flex cursor-pointer items-baseline gap-2 px-3 py-1 text-sm',
              i === activeIndex ? 'bg-surface-dim text-text' : 'text-text-muted'
            ]}
            onpointerdown={(e) => {
              // Keep focus in the input while picking a suggestion.
              e.preventDefault();
              accept(s);
            }}
            onpointerenter={() => (activeIndex = i)}
            data-testid="filter-suggestion"
          >
            <span class="truncate font-mono text-xs">{s.label}</span>
            {#if s.detail}
              <span class="ml-auto truncate text-[11px] text-text-faint">{s.detail}</span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <button
    onclick={() => (popoverOpen = !popoverOpen)}
    class={[
      'relative flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors',
      popoverOpen || clauseCount > 0
        ? 'bg-accent text-accent-text'
        : 'bg-surface text-text-muted hover:text-text'
    ]}
    title="Filter by field"
    aria-label="Filter by field"
    aria-expanded={popoverOpen}
    data-testid="filter-popover-toggle"
  >
    <span class="icon-[uil--filter] size-4"></span>
    {#if clauseCount > 0}
      <span
        class="absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-text px-1 text-[10px] font-semibold text-surface"
        data-testid="filter-count">{clauseCount}</span
      >
    {/if}
  </button>

  {#if popoverOpen}
    <FilterPopover onClose={() => (popoverOpen = false)} />
  {/if}
</div>
