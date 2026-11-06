<script lang="ts">
  import { beansStore, type Bean } from '$lib/beans.svelte';
  import { backlogRoots } from '$lib/backlog';
  import { ui } from '$lib/uiState.svelte';

  let { planningView }: { planningView: 'backlog' | 'board' } = $props();
  import { backlogDrag } from '$lib/backlogDrag.svelte';
  import { planningFilter } from '$lib/planningFilter.svelte';
  import { foldState } from '$lib/foldState.svelte';
  import { parentIds } from '$lib/fold';
  import BeanItem from '$lib/components/BeanItem.svelte';
  import BoardView from '$lib/components/BoardView.svelte';
  import BeanPane from '$lib/components/BeanPane.svelte';
  import SplitPane from '$lib/components/SplitPane.svelte';
  import FilterInput from '$lib/components/FilterInput.svelte';
  import FilterChips from '$lib/components/FilterChips.svelte';
  import ViewToolbar from '$lib/components/ViewToolbar.svelte';

  let filterInput = $state<FilterInput | null>(null);

  // Section roots: top-level backlog beans plus backlog beans whose ancestors
  // are not in the backlog (e.g. todo children of an in-progress parent).
  const todoRoots = $derived(backlogRoots(beansStore.all, 'todo'));
  const draftRoots = $derived(backlogRoots(beansStore.all, 'draft'));

  // Keep roots that match the filter or have a matching descendant.
  function filterBeans(beans: Bean[]) {
    const tree = planningFilter.tree;
    return tree ? beans.filter((bean) => tree.visible.has(bean.id)) : beans;
  }

  const filteredTodoBeans = $derived(filterBeans(todoRoots));
  const filteredDraftBeans = $derived(filterBeans(draftRoots));

  // Every bean with children can be folded; "collapse all" folds all of them.
  const foldableIds = $derived(parentIds(beansStore.all));

  function handleKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && (e.key === 'f' || e.key === '/')) {
      e.preventDefault();
      filterInput?.focus();
      return;
    }
    if (e.key === 'Escape' && ui.currentBean && !ui.showForm) {
      ui.clearSelection();
    }
  }

  function handlePlanningClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      ui.clearSelection();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex h-full flex-col">
  <ViewToolbar showPanelToggles={false}>
    <div class="flex">
      <button
        onclick={() => ui.navigateToPlanningView('backlog')}
        class={[
          'btn-tab rounded-l-md',
          planningView === 'backlog' ? 'btn-tab-active' : 'btn-tab-inactive'
        ]}
      >
        Backlog
      </button>
      <button
        onclick={() => ui.navigateToPlanningView('board')}
        class={[
          'btn-tab rounded-r-md border-l-0',
          planningView === 'board' ? 'btn-tab-active' : 'btn-tab-inactive'
        ]}
      >
        Board
      </button>
    </div>
    <div class="mx-3 w-72 transition-[width] focus-within:w-[32rem]">
      <FilterInput bind:this={filterInput} />
    </div>
    {#snippet right()}
      {#if planningView === 'backlog'}
        <div class="mr-2 flex gap-1">
          <button
            class="btn-icon size-8 hover:bg-surface-dim disabled:cursor-not-allowed disabled:opacity-40"
            title="Collapse all"
            aria-label="Collapse all"
            disabled={foldableIds.length === 0}
            onclick={() => foldState.collapseAll(foldableIds)}
          >
            <span class="icon-[uil--angle-double-up] size-4"></span>
          </button>
          <button
            class="btn-icon size-8 hover:bg-surface-dim disabled:cursor-not-allowed disabled:opacity-40"
            title="Expand all"
            aria-label="Expand all"
            disabled={foldState.collapsedCount === 0}
            onclick={() => foldState.expandAll()}
          >
            <span class="icon-[uil--angle-double-down] size-4"></span>
          </button>
        </div>
      {/if}
      <button class="btn-primary" onclick={() => ui.openCreateForm()}>+ New Bean</button>
    {/snippet}
  </ViewToolbar>
  <FilterChips />

  <div class="flex min-h-0 flex-1 overflow-hidden">
    {#snippet backlogBoard()}
      <div class="flex h-full flex-col bg-surface">
        {#if planningView === 'backlog'}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="min-h-0 flex-1 overflow-auto bg-surface-alt" onclick={handlePlanningClick}>
            {#snippet backlogSection(
              beans: typeof filteredTodoBeans,
              status: string,
              label: string
            )}
              <div
                class="p-3"
                data-backlog-section={status}
                ondragover={(e) => backlogDrag.hoverList(e, null, beans.length, status)}
                ondragleave={(e) => backlogDrag.leaveList(e, e.currentTarget, null)}
                ondrop={(e) => backlogDrag.drop(e, null, beans)}
                role="list"
              >
                <h3 class="mb-2 px-1 text-xs font-semibold tracking-wide text-text-faint uppercase">
                  {label}
                  <span class="font-normal">{beans.length}</span>
                </h3>
                {#each beans as bean, i (bean.id)}
                  <BeanItem
                    {bean}
                    parentId={null}
                    index={i}
                    selectedId={ui.currentBean?.id}
                    onSelect={(b) => ui.selectBean(b)}
                    filter={planningFilter.tree}
                    sectionStatus={status}
                    showParentContext
                  />
                {:else}
                  {#if !beansStore.loading}
                    <p class="text-center py-4 text-sm text-text-muted">No beans</p>
                  {/if}
                {/each}

                <div
                  class={[
                    'mx-1 rounded-full transition-colors',
                    backlogDrag.showEndIndicator(null, beans.length, status)
                      ? 'h-0.5 bg-accent'
                      : 'h-0'
                  ]}
                ></div>
              </div>
            {/snippet}

            {@render backlogSection(filteredTodoBeans, 'todo', 'Todo')}
            {@render backlogSection(filteredDraftBeans, 'draft', 'Draft')}
          </div>
        {:else}
          <BoardView onSelect={(b) => ui.selectBean(b)} selectedId={ui.currentBean?.id} />
        {/if}
      </div>
    {/snippet}

    {#snippet detailPanel()}
      {#if ui.currentBean}
        <BeanPane
          bean={ui.currentBean}
          onSelect={(b) => ui.selectBean(b)}
          onEdit={(b) => ui.openEditForm(b)}
          onClose={() => ui.clearSelection()}
        />
      {/if}
    {/snippet}

    <SplitPane
      direction="horizontal"
      panels={[
        { content: backlogBoard },
        {
          content: detailPanel,
          size: 480,
          collapsed: !ui.currentBean,
          persistKey: 'detail-width'
        }
      ]}
    />
  </div>
</div>
