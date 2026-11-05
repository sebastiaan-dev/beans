<script lang="ts">
  import {
    PRIORITIES,
    STATUSES,
    TYPES,
    blockedSelection,
    clearClauses,
    selectedValues,
    setBlocked,
    setClause,
    toggleValue,
    type FilterField,
    type FilterQuery
  } from '$lib/filter';
  import { planningFilter } from '$lib/planningFilter.svelte';

  let { onClose }: { onClose: () => void } = $props();

  const query = $derived(planningFilter.query);
  const blocked = $derived(blockedSelection(query));
  const tags = $derived(planningFilter.sources.tags);

  const containerBeans = $derived(
    planningFilter.sources.beans.toSorted(
      (a, b) => containerRank(a.type) - containerRank(b.type) || a.title.localeCompare(b.title)
    )
  );
  const ancestorFields: FilterField[] = ['milestone', 'epic', 'parent'];
  const selectedAncestor = $derived(
    ancestorFields.map((f) => selectedValues(query, f)[0]).find(Boolean) ?? ''
  );

  function containerRank(type: string) {
    return type === 'milestone' ? 0 : type === 'epic' ? 1 : 2;
  }

  function setAncestor(id: string) {
    planningFilter.update((q) => {
      let next: FilterQuery = q;
      for (const f of ancestorFields) next = setClause(next, f, []);
      if (!id) return next;
      const target = containerBeans.find((b) => b.id === id);
      const field: FilterField =
        target?.type === 'milestone' ? 'milestone' : target?.type === 'epic' ? 'epic' : 'parent';
      return setClause(next, field, [id]);
    });
  }

  const sections: { field: FilterField; label: string; values: () => readonly string[] }[] = [
    { field: 'status', label: 'Status', values: () => STATUSES },
    { field: 'type', label: 'Type', values: () => TYPES },
    { field: 'priority', label: 'Priority', values: () => PRIORITIES },
    { field: 'tag', label: 'Tags', values: () => tags }
  ];

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
    }
  }
</script>

<div
  class="absolute top-full left-0 z-50 mt-1 w-[30rem] rounded-md border border-border bg-surface p-3 text-sm text-text shadow-lg"
  role="dialog"
  aria-label="Filter beans"
  tabindex="-1"
  onkeydown={handleKeydown}
  data-testid="filter-popover"
>
  <div class="grid grid-cols-4 gap-3">
    {#each sections as section (section.field)}
      {@const selected = selectedValues(query, section.field)}
      <fieldset class="min-w-0" data-filter-section={section.field}>
        <legend class="mb-1 text-xs font-semibold tracking-wide text-text-faint uppercase">
          {section.label}
        </legend>
        <div class="max-h-40 overflow-y-auto">
          {#each section.values() as value (value)}
            <label
              class="flex cursor-pointer items-center gap-1.5 py-0.5 text-text-muted hover:text-text"
            >
              <input
                type="checkbox"
                class="accent-current"
                checked={selected.includes(value)}
                onchange={() => planningFilter.update((q) => toggleValue(q, section.field, value))}
              />
              <span class="truncate">{value}</span>
            </label>
          {:else}
            <p class="text-xs text-text-faint">None</p>
          {/each}
        </div>
      </fieldset>
    {/each}
  </div>

  <div class="mt-3 flex items-end gap-3 border-t border-border pt-3">
    <fieldset>
      <legend class="mb-1 text-xs font-semibold tracking-wide text-text-faint uppercase">
        Blocked
      </legend>
      <div class="flex" data-filter-section="blocked">
        {#each [{ value: null, label: 'Any' }, { value: true, label: 'Yes' }, { value: false, label: 'No' }] as opt, i (opt.label)}
          <button
            class={[
              'btn-tab-sm',
              i === 0 ? 'rounded-l' : i === 2 ? 'rounded-r border-l-0' : 'border-l-0',
              blocked === opt.value ? 'btn-tab-active' : 'btn-tab-sm-inactive'
            ]}
            aria-pressed={blocked === opt.value}
            onclick={() => planningFilter.update((q) => setBlocked(q, opt.value))}
          >
            {opt.label}
          </button>
        {/each}
      </div>
    </fieldset>

    <label class="flex min-w-0 flex-1 flex-col">
      <span class="mb-1 text-xs font-semibold tracking-wide text-text-faint uppercase">
        Below milestone / epic
      </span>
      <select
        class="h-6 min-w-0 rounded border border-border bg-surface px-1 text-xs text-text"
        value={selectedAncestor}
        onchange={(e) => setAncestor(e.currentTarget.value)}
        data-testid="filter-ancestor"
      >
        <option value="">Anywhere</option>
        {#each containerBeans as b (b.id)}
          <option value={b.id}>{b.type}: {b.title}</option>
        {/each}
        {#if selectedAncestor && !containerBeans.some((b) => b.id === selectedAncestor)}
          <option value={selectedAncestor}>{selectedAncestor}</option>
        {/if}
      </select>
    </label>
  </div>

  <div class="mt-3 flex items-center gap-2 border-t border-border pt-2 text-[11px] text-text-faint">
    <span class="min-w-0 flex-1">
      Or type e.g. <code class="font-mono">status:todo,draft priority:&gt;=high tag:ui</code>;
      prefix <code class="font-mono">-</code> to exclude.
    </span>
    <button
      class="shrink-0 cursor-pointer text-xs text-text-muted hover:text-text"
      onclick={() => planningFilter.update(clearClauses)}
      data-testid="filter-popover-clear"
    >
      Clear fields
    </button>
  </div>
</div>
