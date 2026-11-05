<script lang="ts">
  import './layout.css';
  import favicon from '$lib/assets/favicon.svg';
  import { preloadHighlighter } from '$lib/markdown';
  import { page } from '$app/state';
  import { onMount, onDestroy } from 'svelte';
  import { beansStore } from '$lib/beans.svelte';
  import { worktreeStore, MAIN_WORKSPACE_ID } from '$lib/worktrees.svelte';
  import { agentStatusesStore } from '$lib/agentStatuses.svelte';
  import { configStore } from '$lib/config.svelte';
  import { ui } from '$lib/uiState.svelte';
  import { foldState } from '$lib/foldState.svelte';
  import { sidebarState } from '$lib/sidebarState.svelte';
  import { SIDEBAR_RAIL_WIDTH, isSidebarToggleShortcut } from '$lib/sidebar';
  import BeanForm from '$lib/components/BeanForm.svelte';
  import Sidebar from '$lib/components/Sidebar.svelte';
  import SplitPane from '$lib/components/SplitPane.svelte';
  import Toaster from '$lib/components/Toaster.svelte';
  import { toasts } from '$lib/toasts.svelte';

  preloadHighlighter();

  let { data, children } = $props();

  // Initialize UI state from load function data (runs before first render)
  $effect.pre(() => {
    ui.showPlanningChat = data.showPlanningChat;
    ui.showChanges = data.showChanges;
    ui.filterText = data.filterText;
  });

  // Sync UIState from URL path on every navigation
  $effect(() => {
    ui.syncFromUrl(page.url.pathname);
  });

  // Hydrate initial bean selection from URL ?bean= param (once, after activeView is set)
  let initialBeanApplied = false;
  $effect(() => {
    if (!initialBeanApplied && data.selectedBeanId) {
      initialBeanApplied = true;
      ui.selectBeanById(data.selectedBeanId);
    }
  });

  // Fall back to planning view if agents are disabled or the workspace's worktree is removed.
  // Wait for worktreeStore to initialize before checking — otherwise a cold page load
  // to a workspace route would redirect before the subscription delivers data.
  // Workspaces can also disappear because of something outside this view (e.g.
  // `wt remove` in a terminal); tell the user why they were sent away.
  let redirectingFrom: string | null = null;
  $effect(() => {
    if (ui.isPlanning) redirectingFrom = null;
    if (ui.isPlanning || !worktreeStore.initialized) return;
    if (!configStore.agentEnabled) {
      ui.navigateTo('planning');
      return;
    }
    const id = ui.activeView;
    if (id === MAIN_WORKSPACE_ID || worktreeStore.hasWorktree(id)) return;
    // Navigation is async: don't notify twice while it is in flight.
    if (redirectingFrom === id) return;
    redirectingFrom = id;
    const label = worktreeStore.labelFor(id);
    toasts.show(
      label ? `Workspace "${label}" was removed` : `Workspace "${id}" does not exist`,
      { message: label ? 'Its worktree no longer exists, so you are back in Planning.' : undefined }
    );
    ui.forgetWorkspace(id);
    ui.navigateTo('planning');
  });

  // Fold state of bean trees is stored per project.
  $effect(() => {
    if (configStore.projectName) foldState.setProject(configStore.projectName);
  });

  function handleKeydown(e: KeyboardEvent) {
    if (isSidebarToggleShortcut(e)) {
      e.preventDefault();
      sidebarState.toggle();
    }
  }

  onMount(() => {
    configStore.load();
    beansStore.subscribe();
    worktreeStore.subscribe();
    agentStatusesStore.subscribe();
  });

  onDestroy(() => {
    beansStore.unsubscribe();
    worktreeStore.unsubscribe();
    agentStatusesStore.unsubscribe();
  });
</script>

<svelte:head>
  <title>{configStore.projectName ? `${configStore.projectName} — Beans UI` : 'Beans UI'}</title>
  <link rel="icon" href={favicon} />
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

<div class="flex h-screen flex-col bg-surface-alt">
  {#if beansStore.error}
    <div class="m-4">
      <div class="rounded-lg border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
        Error: {beansStore.error}
      </div>
    </div>
  {:else}
    {#snippet sidebarPanel()}
      <Sidebar />
    {/snippet}

    {#snippet mainContent()}
      {@render children()}
    {/snippet}

    <SplitPane
      direction="horizontal"
      panels={[
        {
          content: sidebarPanel,
          size: 224,
          minSize: 150,
          maxSize: 400,
          persistKey: 'sidebar',
          fixedSize: sidebarState.collapsed ? SIDEBAR_RAIL_WIDTH : undefined
        },
        { content: mainContent }
      ]}
    />
  {/if}
</div>

<Toaster />

{#if ui.showForm}
  <BeanForm
    bean={ui.editingBean}
    onClose={() => ui.closeForm()}
    onSaved={(bean) => ui.selectBean(bean)}
  />
{/if}
