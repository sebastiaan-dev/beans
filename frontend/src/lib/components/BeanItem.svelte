<script lang="ts">
  import type { Bean } from '$lib/beans.svelte';
  import { beansStore } from '$lib/beans.svelte';
  import { backlogDrag } from '$lib/backlogDrag.svelte';
  import type { TreeFilterResult } from '$lib/filter';
  import BeanCard from './BeanCard.svelte';
  import BeanItem from './BeanItem.svelte';

  interface Props {
    bean: Bean;
    /** Parent ID of this bean's sibling group (null = top-level) */
    parentId?: string | null;
    index?: number;
    depth?: number;
    selectedId?: string | null;
    onSelect?: (bean: Bean) => void;
    /** Active planning filter (null = unfiltered): hides non-matching subtrees. */
    filter?: TreeFilterResult | null;
    /** Status of the backlog section this bean is in (for cross-section drag) */
    sectionStatus?: string;
    /** Show the bean's parent when it is rendered outside its parent's subtree */
    showParentContext?: boolean;
  }

  let {
    bean,
    parentId = null,
    index = 0,
    depth = 0,
    selectedId = null,
    onSelect,
    filter = null,
    sectionStatus,
    showParentContext = false
  }: Props = $props();

  const contextParent = $derived(
    showParentContext && bean.parentId ? beansStore.get(bean.parentId) : undefined
  );

  const children = $derived(beansStore.children(bean.id));
  const filteredChildren = $derived(
    filter ? children.filter((child) => filter.visible.has(child.id)) : children
  );
  /** Shown only as context for a matching descendant. */
  const contextOnly = $derived(!!filter && !filter.matched.has(bean.id));

  function handleClick(e: MouseEvent) {
    e.stopPropagation();
    onSelect?.(bean);
  }
</script>

<div class="bean-item my-1" data-bean-id={bean.id} data-filter-context={contextOnly || undefined}>
  <!-- Drop indicator before this card -->
  <div
    class={[
      'mx-1 rounded-full transition-colors',
      backlogDrag.showIndicator(parentId, index, bean.id, sectionStatus) ? 'h-0.5 bg-accent' : 'h-0'
    ]}
  ></div>

  {#if contextParent}
    <div
      class="flex min-w-0 items-center gap-1 px-1 pb-0.5 text-[10px] text-text-faint"
      data-parent-context
      title="Parent: {contextParent.title} ({contextParent.status})"
    >
      <span class="icon-[uil--corner-down-right-alt] size-3 shrink-0"></span>
      <span class="truncate">{contextParent.title}</span>
      <span class="shrink-0">· {contextParent.status}</span>
    </div>
  {/if}

  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class={[
      'rounded transition-all',
      backlogDrag.draggedBeanId === bean.id && 'opacity-40',
      contextOnly && 'opacity-60',
      backlogDrag.isReparentTarget(bean.id) && 'ring-2 ring-accent ring-offset-1'
    ]}
    draggable="true"
    ondragstart={(e) => backlogDrag.startDrag(e, bean)}
    ondragend={() => backlogDrag.endDrag()}
    ondragover={(e) => backlogDrag.hoverCard(e, parentId, index, bean.id, sectionStatus)}
    onclick={handleClick}
  >
    <BeanCard
      {bean}
      variant="list"
      selected={selectedId === bean.id}
      onclick={() => onSelect?.(bean)}
    />
  </div>

  {#if filteredChildren.length > 0}
    <div
      class="ml-6"
      ondragover={(e) => backlogDrag.hoverList(e, bean.id, filteredChildren.length)}
      ondragleave={(e) => backlogDrag.leaveList(e, e.currentTarget, bean.id)}
      ondrop={(e) => backlogDrag.drop(e, bean.id, filteredChildren)}
      role="list"
    >
      {#each filteredChildren as child, i (child.id)}
        <BeanItem
          bean={child}
          parentId={bean.id}
          index={i}
          depth={depth + 1}
          {selectedId}
          {onSelect}
          {filter}
          {sectionStatus}
        />
      {/each}

      <!-- Drop indicator at end of children -->
      <div
        class={[
          'mx-1 rounded-full transition-colors',
          backlogDrag.showEndIndicator(bean.id, filteredChildren.length)
            ? 'h-0.5 bg-accent'
            : 'h-0'
        ]}
      ></div>
    </div>
  {/if}
</div>
