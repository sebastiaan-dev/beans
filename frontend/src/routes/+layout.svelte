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
  import FocusView from '$lib/components/FocusView.svelte';
  import { focusMode } from '$lib/focusMode.svelte';
  import { focusActionForKey, isFocusEnterShortcut } from '$lib/focusMode';

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
    toasts.show(label ? `Workspace "${label}" was removed` : `Workspace "${id}" does not exist`, {
      message: label ? 'Its worktree no longer exists, so you are back in Planning.' : undefined
    });
    ui.forgetWorkspace(id);
    ui.navigateTo('planning');
  });

  // Fold state of bean trees is stored per project.
  $effect(() => {
    if (configStore.projectName) foldState.setProject(configStore.projectName);
  });

  // Focus mode: leave it with a toast when the focused bean disappears
  // (deleted, or a link to a bean that doesn't exist).
  const FOCUS_MISSING_GRACE_MS = 2000;
  let lastFocused: { id: string; title: string } | null = null;
  let exitingMissing: string | null = null;
  $effect(() => {
    const id = focusMode.id;
    if (!id || beansStore.loading) return;
    const bean = focusMode.bean;
    if (bean) {
      lastFocused = { id, title: bean.title };
      exitingMissing = null;
      return;
    }
    // Navigation is async: don't notify twice while it is in flight.
    if (exitingMissing === id) return;
    const leave = (title: string, message: string) => {
      exitingMissing = id;
      toasts.show(title, { message });
      focusMode.exit();
    };
    if (lastFocused?.id === id) {
      leave(`"${lastFocused.title}" was deleted`, 'Left focus mode.');
      return;
    }
    // Never seen: give a just-created bean (e.g. a link from an agent) a moment
    // to arrive from the file watcher before giving up.
    const timer = setTimeout(
      () => leave(`Bean "${id}" not found`, 'It may have been deleted.'),
      FOCUS_MISSING_GRACE_MS
    );
    return () => clearTimeout(timer);
  });

  // When focus mode ends (Esc, Back, browser back), select the bean that was
  // focused last and restore keyboard focus in the regular view.
  let previousFocusId: string | null = null;
  $effect(() => {
    const id = focusMode.id;
    const prev = previousFocusId;
    previousFocusId = id;
    if (id || !prev) return;
    exitingMissing = null;
    if (beansStore.get(prev) && ui.selectedBeanId !== prev) ui.selectBeanById(prev);
    focusMode.restoreFocus();
  });

  /** Whether a modal dialog (bean form, confirmation, …) has the keyboard. */
  function modalOpen(): boolean {
    return ui.showForm || !!document.querySelector('[aria-modal="true"]');
  }

  function handleFocusModeKeydown(e: KeyboardEvent) {
    if (modalOpen()) return;
    const action = focusActionForKey(e);
    if (!action) return;
    e.preventDefault();
    switch (action) {
      case 'exit':
        focusMode.exit();
        break;
      case 'prev':
      case 'next':
        focusMode.step(action);
        break;
      case 'edit':
        if (focusMode.bean) ui.openEditForm(focusMode.bean);
        break;
      case 'toggleWidth':
        focusMode.toggleWidth();
        break;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (focusMode.active) {
      handleFocusModeKeydown(e);
      return;
    }
    if (isSidebarToggleShortcut(e)) {
      e.preventDefault();
      sidebarState.toggle();
      return;
    }
    if (isFocusEnterShortcut(e) && ui.currentBean && !modalOpen()) {
      e.preventDefault();
      focusMode.enter(ui.currentBean.id);
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
  <title
    >{[focusMode.bean?.title, configStore.projectName, 'Beans UI']
      .filter(Boolean)
      .join(' — ')}</title
  >
  <link rel="icon" href={favicon} />
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

<!-- Kept mounted under focus mode (inert and hidden) so scroll positions and state survive. -->
<div
  class={['flex h-screen flex-col bg-surface-alt', focusMode.active && 'invisible']}
  inert={focusMode.active}
>
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

{#if focusMode.active && !beansStore.error}
  <FocusView />
{/if}

<Toaster />

{#if ui.showForm}
  <BeanForm
    bean={ui.editingBean}
    onClose={() => ui.closeForm()}
    onSaved={(bean) => ui.selectBean(bean)}
  />
{/if}
