<script lang="ts">
  import type { Bean } from '$lib/beans.svelte';
  import { sortBeans } from '$lib/beans.svelte';
  import { isArchived } from '$lib/filter';
  import { applyDrop } from '$lib/dragOrder';
  import { planningFilter } from '$lib/planningFilter.svelte';
  import { client } from '$lib/graphqlClient';
  import { typeBorders } from '$lib/styles';
  import { fade } from 'svelte/transition';
  import { ArchiveBeanDocument } from '$lib/graphql/generated';
  import BeanCard from './BeanCard.svelte';
  import ConfirmModal from './ConfirmModal.svelte';

  interface Props {
    onSelect?: (bean: Bean) => void;
    selectedId?: string | null;
  }

  let { onSelect, selectedId = null }: Props = $props();

  let confirmingArchiveAll = $state(false);
  let archivingAll = $state(false);

  async function archiveAll() {
    archivingAll = true;
    for (const bean of archivableBeans) {
      await client.mutation(ArchiveBeanDocument, { id: bean.id }).toPromise();
    }
    archivingAll = false;
    confirmingArchiveAll = false;
  }

  const columns = [
    { status: 'todo', label: 'Todo', color: 'bg-status-todo-bg text-status-todo-text' },
    {
      status: 'in-progress',
      label: 'In Progress',
      color: 'bg-status-in-progress-bg text-status-in-progress-text'
    },
    {
      status: 'completed',
      label: 'Completed',
      color: 'bg-status-completed-bg text-status-completed-text'
    }
  ];

  function beansForStatus(status: string): Bean[] {
    // sortBeans already handles order → priority → type → title sorting
    return sortBeans(
      planningFilter.beans.filter(
        (b) => b.status === status && b.status !== 'scrapped' && planningFilter.predicate(b)
      )
    );
  }

  // The completed column also lists archived beans when the filter asks for them.
  const archivableBeans = $derived(beansForStatus('completed').filter((b) => !isArchived(b)));

  // Drag and drop
  let draggedBeanId = $state<string | null>(null);
  let dropTargetStatus = $state<string | null>(null);
  let dropIndex = $state<number | null>(null);

  function onDragStart(e: DragEvent, bean: Bean) {
    draggedBeanId = bean.id;
    e.dataTransfer!.effectAllowed = 'move';
    e.dataTransfer!.setData('text/plain', bean.id);
  }

  function onDragEnd() {
    draggedBeanId = null;
    dropTargetStatus = null;
    dropIndex = null;
  }

  function onCardDragOver(e: DragEvent, status: string, index: number) {
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer!.dropEffect = 'move';
    dropTargetStatus = status;

    // Determine if we're in the top or bottom half of the card
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const midY = rect.top + rect.height / 2;
    dropIndex = e.clientY < midY ? index : index + 1;
  }

  function onColumnDragOver(e: DragEvent, status: string, beanCount: number) {
    e.preventDefault();
    e.dataTransfer!.dropEffect = 'move';
    dropTargetStatus = status;
    // If dragging over empty space at the bottom, drop at end
    if (dropIndex === null || dropTargetStatus !== status) {
      dropIndex = beanCount;
    }
  }

  function onDragLeave(e: DragEvent, columnEl: HTMLElement) {
    if (!columnEl.contains(e.relatedTarget as Node)) {
      dropTargetStatus = null;
      dropIndex = null;
    }
  }

  function onDrop(e: DragEvent, targetStatus: string, beans: Bean[]) {
    e.preventDefault();
    const targetIdx = dropIndex;
    dropTargetStatus = null;
    dropIndex = null;

    const beanId = draggedBeanId;
    draggedBeanId = null;

    if (!beanId) return;

    applyDrop(beans, beanId, targetIdx ?? beans.length, { newStatus: targetStatus });
  }
</script>

<div class="min-h-0 flex-1 overflow-auto bg-surface-alt px-4 pt-4">
  <div class="flex w-full max-w-4xl min-w-0 items-start">
    {#each columns as col (col.status)}
      {@const beans = beansForStatus(col.status)}
      <div class="min-w-50 flex-1" data-status={col.status}>
        <!-- Column header -->
        <div class="sticky top-0 z-10 mb-3 flex items-center gap-2 bg-surface-alt px-1 pb-1">
          <span class={['badge', col.color]}>{col.label}</span>
          <span class="text-xs text-text-faint">{beans.length}</span>
          {#if col.status === 'completed' && archivableBeans.length > 0}
            <button
              class="cursor-pointer text-text-faint transition-colors hover:text-text-muted"
              title="Archive all completed beans"
              onclick={() => (confirmingArchiveAll = true)}
              disabled={archivingAll}
            >
              <span class="icon-[uil--archive] size-3.5"></span>
            </button>
          {/if}
        </div>

        <!-- Cards (drop zone) -->
        <div
          class={[
            'rounded-xl p-2 transition-colors',
            dropTargetStatus === col.status && draggedBeanId && 'bg-accent/10 ring-2 ring-accent/30'
          ]}
          role="list"
          ondragover={(e) => onColumnDragOver(e, col.status, beans.length)}
          ondragleave={(e) => onDragLeave(e, e.currentTarget)}
          ondrop={(e) => onDrop(e, col.status, beans)}
        >
          {#each beans as bean, index (bean.id)}
            <!-- Drop indicator (always present, transparent unless active) -->
            <div
              class={[
                'mx-1 my-1 h-0.5 rounded-full transition-colors',
                dropTargetStatus === col.status &&
                draggedBeanId &&
                draggedBeanId !== bean.id &&
                dropIndex === index
                  ? 'bg-accent'
                  : 'bg-transparent'
              ]}
            ></div>

            <div
              class={[
                'overflow-hidden rounded border border-l-5 border-border bg-surface shadow transition-all',
                typeBorders[bean.type] ?? 'border-l-type-task-border',
                draggedBeanId === bean.id ? 'opacity-40' : 'hover:shadow-md',
                selectedId === bean.id && 'bg-accent/5 ring-1 ring-accent'
              ]}
              draggable="true"
              ondragstart={(e) => onDragStart(e, bean)}
              ondragend={onDragEnd}
              ondragover={(e) => onCardDragOver(e, col.status, index)}
              role="listitem"
              transition:fade={{ duration: 150 }}
            >
              <BeanCard {bean} variant="board" onclick={() => onSelect?.(bean)} />
            </div>
          {:else}
            <div class="text-center text-text-faint text-sm py-8">No beans</div>
          {/each}

          <!-- Drop indicator at end (always present) -->
          <div
            class={[
              'mx-1 my-1 h-0.5 rounded-full transition-colors',
              dropTargetStatus === col.status && draggedBeanId && dropIndex === beans.length
                ? 'bg-accent'
                : 'bg-transparent'
            ]}
          ></div>
        </div>
      </div>
    {/each}
  </div>
</div>

{#if confirmingArchiveAll}
  {@const completedCount = archivableBeans.length}
  <ConfirmModal
    title="Archive All Completed"
    message="Are you sure you want to archive all {completedCount} completed beans? This will move them to the archive directory."
    confirmLabel="Archive All"
    danger={false}
    onConfirm={archiveAll}
    onCancel={() => (confirmingArchiveAll = false)}
  />
{/if}
