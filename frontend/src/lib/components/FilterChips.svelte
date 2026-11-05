<script lang="ts">
  import { beansStore } from '$lib/beans.svelte';
  import {
    PARENT_FIELDS,
    clearClauses,
    invalidValues,
    refersTo,
    removeTerm,
    type FilterClause
  } from '$lib/filter';
  import { planningFilter } from '$lib/planningFilter.svelte';

  /** Field clauses with their index in the query, so chips can remove them. */
  const chips = $derived(
    planningFilter.query.terms.flatMap((term, index) =>
      term.kind === 'clause' && term.values.length > 0 ? [{ clause: term, index }] : []
    )
  );

  const OPS: Record<string, string> = { '>=': '≥', '<=': '≤', '>': '>', '<': '<' };

  function operator(clause: FilterClause): string {
    if (clause.op !== '=') return (clause.negate ? 'not ' : '') + OPS[clause.op];
    return clause.negate ? 'is not' : PARENT_FIELDS.includes(clause.field) ? 'below' : 'is';
  }

  function displayValue(clause: FilterClause, value: string): string {
    if (!PARENT_FIELDS.includes(clause.field)) return value;
    const target = beansStore.all.find(
      (b) => (clause.field === 'parent' || b.type === clause.field) && refersTo(b, value)
    );
    return target?.title ?? value;
  }
</script>

{#if chips.length > 0}
  <div
    class="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-border bg-surface-alt px-3 py-1.5"
    data-testid="filter-chips"
  >
    {#each chips as { clause, index } (index)}
      {@const invalid = invalidValues(clause)}
      <span
        class={[
          'inline-flex max-w-80 items-center gap-1 rounded-full border py-0.5 pr-1 pl-2.5 text-xs',
          invalid.length > 0
            ? 'border-warning text-warning'
            : 'border-border bg-surface text-text-muted'
        ]}
        title={invalid.length > 0
          ? `Unknown value(s): ${invalid.join(', ')}`
          : `${clause.field} ${operator(clause)} ${clause.values.join(', ')}`}
        data-testid="filter-chip"
        data-field={clause.field}
      >
        <span class="font-medium text-text">{clause.field}</span>
        <span>{operator(clause)}</span>
        <span class="truncate">{clause.values.map((v) => displayValue(clause, v)).join(', ')}</span>
        <button
          class="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-full hover:bg-surface-dim hover:text-text"
          title="Remove filter"
          aria-label="Remove {clause.field} filter"
          onclick={() => planningFilter.update((q) => removeTerm(q, index))}
          data-testid="filter-chip-remove"
        >
          <span class="icon-[uil--times] size-3"></span>
        </button>
      </span>
    {/each}
    {#if chips.length > 1}
      <button
        class="cursor-pointer px-1 text-xs text-text-faint hover:text-text"
        onclick={() => planningFilter.update(clearClauses)}
        data-testid="filter-chips-clear"
      >
        Clear all
      </button>
    {/if}
  </div>
{/if}
