<script lang="ts">
  import { fade } from 'svelte/transition';
  import { onDestroy } from 'svelte';
  import { pipe, subscribe as wonkaSubscribe } from 'wonka';
  import { worktreeStore, MAIN_WORKSPACE_ID, type WorktreeStatus } from '$lib/worktrees.svelte';
  import { WorkspaceStatusesDocument } from '$lib/graphql/generated';
  import { beansStore, type Bean } from '$lib/beans.svelte';
  import { agentStatusesStore } from '$lib/agentStatuses.svelte';
  import { configStore } from '$lib/config.svelte';
  import { client } from '$lib/graphqlClient';
  import { ui } from '$lib/uiState.svelte';
  import { typeBorders } from '$lib/styles';
  import { decryptText } from '$lib/actions/decryptText';
  import { sidebarState } from '$lib/sidebarState.svelte';
  import { sidebarShortcutLabel, workspaceInitials } from '$lib/sidebar';
  import { isDetached, workspaceLabel, workspaceTooltip } from '$lib/workspaces';
  import { toasts } from '$lib/toasts.svelte';
  import ConfirmModal from './ConfirmModal.svelte';
  import CreateWorkspaceModal from './CreateWorkspaceModal.svelte';

  interface PullRequestInfo {
    state: string;
    checkStatus: string;
    number: number;
    url: string;
  }

  interface WorkspaceItem {
    id: string;
    label: string;
    /** Branch shown under the label when it differs from it (null: don't show). */
    branchHint: string | null;
    /** Detached HEAD: the worktree has no branch checked out. */
    detached: boolean;
    /** Hover text with branch and path (null for the main workspace). */
    tooltip: string | null;
    description: string | null;
    beans: Bean[];
    settingUp: boolean;
    setupError: string | null;
    pullRequest: PullRequestInfo | null;
  }

  /** Look up full Bean objects for a worktree's detected bean IDs. */
  function beansForWorktree(beanIds: string[]): Bean[] {
    return beanIds
      .map((id) => beansStore.get(id))
      .filter((b): b is Bean => b != null);
  }

  const mainWorkspace: WorkspaceItem = $derived({
    id: MAIN_WORKSPACE_ID,
    label: configStore.mainBranch,
    branchHint: null,
    detached: false,
    tooltip: null,
    description: null,
    beans: [],
    settingUp: false,
    setupError: null,
    pullRequest: null
  });

  const workspaceItems = $derived([
    mainWorkspace,
    ...worktreeStore.worktrees.map((wt): WorkspaceItem => ({
      id: wt.id,
      label: workspaceLabel(wt),
      // Worktrunk workspaces can be on any branch; show it unless the label says it all.
      branchHint:
        configStore.isWorktrunk && wt.branch && wt.branch !== workspaceLabel(wt) ? wt.branch : null,
      detached: isDetached(wt),
      tooltip: workspaceTooltip(wt),
      description: wt.description ?? null,
      beans: beansForWorktree(wt.beanIds),
      settingUp: wt.setupStatus === 'RUNNING',
      setupError:
        wt.setupStatus === 'FAILED' ? wt.setupError || 'Workspace setup failed.' : null,
      pullRequest: wt.pullRequest ? {
        state: wt.pullRequest.state,
        checkStatus: wt.pullRequest.checkStatus,
        number: wt.pullRequest.number,
        url: wt.pullRequest.url,
      } : null,
    }))
  ]);

  // Track which items have been seen so only genuinely new items animate.
  // Items present when stores finish their initial load are seeded into the
  // set without animation; anything arriving afterwards gets the effect.
  let seenWorkspaceIds = new Set<string>();
  let seenBeanIds = new Set<string>();
  let workspacesSettled = $state(false);
  let beansSettled = $state(false);

  $effect(() => {
    if (!workspacesSettled && worktreeStore.initialized) {
      // Seed with whatever is in the store at the end of initial load
      seenWorkspaceIds = new Set(worktreeStore.worktrees.map((wt) => wt.id));
      workspacesSettled = true;
    }
  });

  $effect(() => {
    if (!beansSettled && !beansStore.loading) {
      seenBeanIds = new Set(beansStore.all.filter((b) => b.worktreeId).map((b) => b.id));
      beansSettled = true;
    }
  });

  function isWorkspaceSeen(id: string): boolean {
    if (!workspacesSettled) return true; // still loading — skip animation
    if (seenWorkspaceIds.has(id)) return true;
    seenWorkspaceIds.add(id);
    return false;
  }

  function isBeanSeen(id: string): boolean {
    if (!beansSettled) return true;
    if (seenBeanIds.has(id)) return true;
    seenBeanIds.add(id);
    return false;
  }

  // Subscribe to workspace git statuses (main repo + worktrees)
  let mainHasChanges = $state(false);
  let worktreeStatuses = $state(new Map<string, WorktreeStatus>());
  let readyWorktreeIds = $derived(new Set(
    [...worktreeStatuses.entries()]
      .filter(([, s]) => s.hasChanges || s.hasUnmergedCommits)
      .map(([id]) => id)
  ));

  const { unsubscribe: unsubStatuses } = pipe(
    client.subscription(WorkspaceStatusesDocument, {}),
    wonkaSubscribe((result) => {
      if (!result.data?.workspaceStatuses) return;
      const statuses = new Map<string, WorktreeStatus>();
      for (const ws of result.data.workspaceStatuses) {
        if (ws.id === MAIN_WORKSPACE_ID) {
          mainHasChanges = ws.hasChanges;
        } else {
          statuses.set(ws.id, { hasChanges: ws.hasChanges, hasUnmergedCommits: ws.hasUnmergedCommits });
        }
      }
      worktreeStatuses = statuses;
    })
  );
  onDestroy(unsubStatuses);

  let confirmingRemoveId = $state<string | null>(null);
  let confirmingStatus = $state<WorktreeStatus | null>(null);

  function promptDestroy(id: string) {
    // Show modal immediately with cached status, then refresh in background
    confirmingStatus = worktreeStatuses.get(id) ?? null;
    confirmingRemoveId = id;
    // Fetch fresh status so warnings update if cache was stale
    worktreeStore.getWorktreeStatus(id).then((fresh) => {
      if (confirmingRemoveId === id) {
        confirmingStatus = fresh;
      }
    });
  }

  let creatingWorkspace = $state(false);

  async function handleCreateWorktree() {
    // The backend decides between one-click creation and choosing a name.
    await configStore.ready;
    // With worktrunk the name becomes the branch, so let the user choose it.
    if (configStore.isWorktrunk) {
      creatingWorkspace = true;
      return;
    }
    const wt = await worktreeStore.createWorktree();
    if (wt) {
      ui.navigateTo(wt.id);
    } else {
      toasts.error('Could not create the workspace', worktreeStore.error ?? undefined);
    }
  }

  async function handleRemoveWorktree(id: string) {
    confirmingRemoveId = null;
    confirmingStatus = null;
    if (ui.activeView === id) {
      ui.navigateTo('planning');
    }
    // The backend stops the run process and terminal sessions before removing the worktree.
    const label = worktreeStore.labelFor(id) ?? id;
    if (await worktreeStore.removeWorktree(id)) {
      ui.forgetWorkspace(id);
    } else {
      toasts.error(`Could not destroy workspace "${label}"`, worktreeStore.error ?? undefined);
    }
  }

  const shortcut = sidebarShortcutLabel(
    typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
  );

  /** Compact status indicator for a workspace in the collapsed rail. */
  function railStatus(item: WorkspaceItem): { class: string; label: string } | null {
    if (item.settingUp) return { class: 'bg-text-faint animate-pulse', label: 'Setting up' };
    if (item.setupError) return { class: 'bg-danger', label: 'Setup failed' };
    if (item.id === MAIN_WORKSPACE_ID) {
      return mainHasChanges ? { class: 'bg-warning', label: 'Uncommitted changes' } : null;
    }
    const pr = item.pullRequest;
    if (configStore.worktreeIntegrateMode === 'pr' && pr) {
      if (pr.state === 'merged') return { class: 'bg-purple-400', label: `PR #${pr.number} merged` };
      if (pr.checkStatus === 'fail') return { class: 'bg-danger', label: `PR #${pr.number}: checks failed` };
      if (pr.checkStatus === 'pending') return { class: 'bg-warning', label: `PR #${pr.number}: checks running` };
      return { class: 'bg-success', label: `PR #${pr.number}: checks passed` };
    }
    if (readyWorktreeIds.has(item.id)) return { class: 'bg-success', label: 'Ready to integrate' };
    return null;
  }

  function railTooltip(item: WorkspaceItem): string {
    const status = railStatus(item);
    const running = agentStatusesStore.isRunning(item.id) ? 'Agent running' : null;
    const extras = [item.description, running, status?.label];
    return item.tooltip
      ? [item.tooltip, ...extras].filter(Boolean).join('\n')
      : [item.label, ...extras].filter(Boolean).join(' — ');
  }
</script>

{#snippet planningIcon()}
  <svg
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 20 20"
    fill="currentColor"
    class="h-4 w-4 shrink-0"
  >
    <path
      fill-rule="evenodd"
      d="M6 4.75A.75.75 0 016.75 4h10.5a.75.75 0 010 1.5H6.75A.75.75 0 016 4.75zM6 10a.75.75 0 01.75-.75h10.5a.75.75 0 010 1.5H6.75A.75.75 0 016 10zm0 5.25a.75.75 0 01.75-.75h10.5a.75.75 0 010 1.5H6.75a.75.75 0 01-.75-.75zM1.99 4.75a1 1 0 011-1h.01a1 1 0 010 2h-.01a1 1 0 01-1-1zm0 5.25a1 1 0 011-1h.01a1 1 0 010 2h-.01a1 1 0 01-1-1zm0 5.25a1 1 0 011-1h.01a1 1 0 010 2h-.01a1 1 0 01-1-1z"
      clip-rule="evenodd"
    />
  </svg>
{/snippet}

{#snippet plusIcon()}
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="h-3.5 w-3.5">
    <path d="M8.75 3.75a.75.75 0 0 0-1.5 0v3.5h-3.5a.75.75 0 0 0 0 1.5h3.5v3.5a.75.75 0 0 0 1.5 0v-3.5h3.5a.75.75 0 0 0 0-1.5h-3.5v-3.5Z" />
  </svg>
{/snippet}

<nav
  class="flex h-full flex-col bg-surface-alt"
  aria-label="Sidebar"
  data-collapsed={sidebarState.collapsed}
>
  <div
    class={[
      'flex h-14 shrink-0 items-center border-b border-border',
      sidebarState.collapsed ? 'justify-center px-1' : 'gap-2 p-3'
    ]}
  >
    {#if !sidebarState.collapsed}
      <span class="min-w-0 flex-1 truncate text-sm font-semibold text-text"
        >{configStore.projectName || 'beans'}</span
      >
    {/if}
    <button
      onclick={() => sidebarState.toggle()}
      class="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded text-text-faint transition-colors hover:bg-surface hover:text-text"
      aria-label={sidebarState.collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
      aria-expanded={!sidebarState.collapsed}
      title="{sidebarState.collapsed ? 'Expand' : 'Collapse'} sidebar ({shortcut})"
      data-sidebar-toggle
    >
      <span
        class={[
          'block size-4',
          sidebarState.collapsed ? 'icon-[uil--angle-double-right]' : 'icon-[uil--angle-double-left]'
        ]}
      ></span>
    </button>
  </div>

  {#if sidebarState.collapsed}
    <!-- Collapsed icon rail: same navigation, labels in tooltips -->
    <div class="flex flex-1 flex-col items-center gap-1 overflow-x-hidden overflow-y-auto py-2">
      <button
        onclick={() => ui.navigateTo('planning')}
        class={[
          'relative flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors',
          ui.isPlanning
            ? 'bg-surface text-text'
            : 'text-text-muted hover:bg-surface hover:text-text'
        ]}
        aria-label="Planning"
        aria-current={ui.isPlanning ? 'page' : undefined}
        title="Planning"
      >
        {@render planningIcon()}
        {#if configStore.agentEnabled && agentStatusesStore.isRunning(MAIN_WORKSPACE_ID)}
          <span class="loader absolute -top-0.5 -right-0.5 !size-2.5" transition:fade={{ duration: 200 }}></span>
        {/if}
      </button>

      {#if configStore.agentEnabled}
        <div class="my-1 h-px w-7 shrink-0 bg-border" role="separator"></div>
        <button
          onclick={handleCreateWorktree}
          class="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded text-text-faint transition-colors hover:bg-surface hover:text-text"
          aria-label="Create worktree"
          title="Create workspace"
        >
          {@render plusIcon()}
        </button>

        {#each workspaceItems as item (item.id)}
          {@const status = railStatus(item)}
          {@const active = ui.activeView === item.id}
          <button
            onclick={() => ui.navigateTo(item.id)}
            transition:fade={{ duration: 150 }}
            class={[
              'relative flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-md border text-[11px] font-semibold transition-colors',
              worktreeStore.isDestroying(item.id) && 'pointer-events-none opacity-30',
              active
                ? 'border-accent/40 bg-surface text-text'
                : 'border-border/50 bg-surface/50 text-text-muted hover:border-border hover:bg-surface hover:text-text'
            ]}
            aria-label={item.label}
            aria-current={active ? 'page' : undefined}
            title={railTooltip(item)}
            data-workspace-id={item.id}
          >
            {workspaceInitials(item.label)}
            {#if agentStatusesStore.isRunning(item.id)}
              <span class="loader absolute -top-0.5 -right-0.5 !size-2.5" transition:fade={{ duration: 200 }}></span>
            {:else if status}
              <span class={['absolute -top-0.5 -right-0.5 size-2 rounded-full', status.class]}></span>
            {/if}
          </button>
        {/each}
      {/if}
    </div>
  {:else}
  <div class="flex-1 overflow-y-auto p-2">
    <!-- Planning item -->
    <button
      onclick={() => ui.navigateTo('planning')}
      class={[
        'flex w-full cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors',
        ui.isPlanning
          ? 'bg-surface font-medium text-text'
          : 'text-text-muted hover:bg-surface hover:text-text'
      ]}
    >
      {@render planningIcon()}
      Planning
      {#if configStore.agentEnabled && agentStatusesStore.isRunning(MAIN_WORKSPACE_ID)}
        <div class="loader ml-auto shrink-0" transition:fade={{ duration: 200 }}></div>
      {/if}
    </button>

    {#if configStore.agentEnabled}
      <!-- Workspaces section -->
      <div class="mt-4 mb-1 flex items-center justify-between px-3">
        <span class="text-xs font-medium tracking-wider text-text-faint uppercase">
          Workspaces
        </span>
        <button
          onclick={handleCreateWorktree}
          class="cursor-pointer rounded p-0.5 text-text-faint transition-colors hover:bg-surface hover:text-text"
          aria-label="Create worktree"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 16 16"
            fill="currentColor"
            class="h-3.5 w-3.5"
          >
            <path d="M8.75 3.75a.75.75 0 0 0-1.5 0v3.5h-3.5a.75.75 0 0 0 0 1.5h3.5v3.5a.75.75 0 0 0 1.5 0v-3.5h3.5a.75.75 0 0 0 0-1.5h-3.5v-3.5Z" />
          </svg>
        </button>
      </div>

      <div class="flex flex-col gap-1">
      {#each workspaceItems as item (item.id)}
        {@const destroying = worktreeStore.isDestroying(item.id)}
        <div
          transition:fade={{ duration: 150 }}
          class={[
            'rounded-md border transition-all',
            destroying && 'pointer-events-none opacity-30',
            ui.activeView === item.id
              ? 'border-accent/30 bg-surface'
              : 'border-border/50 bg-surface/50 hover:border-border hover:bg-surface'
          ]}
        >
          <div class="group flex items-start">
            <button
              onclick={() => ui.navigateTo(item.id)}
              title={item.tooltip ?? undefined}
              data-workspace-id={item.id}
              class={[
                'flex min-w-0 flex-1 cursor-pointer items-center gap-2 px-3 py-2 text-left text-sm transition-colors',
                ui.activeView === item.id
                  ? 'font-medium text-text'
                  : 'text-text-muted hover:text-text'
              ]}
            >
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 items-center gap-1.5">
                  <span class="block truncate">{item.label}</span>
                  {#if item.detached}
                    <span
                      class="shrink-0 rounded border border-border px-1 font-mono text-[10px] leading-4 font-normal text-text-faint"
                      title="Detached HEAD: no branch is checked out in this worktree"
                      data-detached
                    >detached</span>
                  {/if}
                </div>
                {#if item.branchHint}
                  <div class="flex min-w-0 items-center gap-1 text-[11px] font-normal text-text-faint" data-branch>
                    <span class="icon-[uil--code-branch] block size-3 shrink-0"></span>
                    <div class="truncate font-mono">{item.branchHint}</div>
                  </div>
                {/if}
                {#if item.settingUp}
                  <span class="block text-xs font-normal text-text-faint animate-pulse">Setting up...</span>
                {:else if item.setupError}
                  <span class="block text-xs font-normal text-danger" title={item.setupError} data-setup-failed>Setup failed</span>
                {:else if item.description}
                  <span class="block text-xs font-normal text-text-faint" use:decryptText={{ text: item.description, immediate: isWorkspaceSeen(item.id) }}></span>
                {/if}
              </div>
            </button>
            <div class="relative mt-2.5 mr-2 h-4 w-4 shrink-0">
              {#if agentStatusesStore.isRunning(item.id)}
                <div class="loader absolute inset-0" transition:fade={{ duration: 200 }}></div>
              {:else if item.id === MAIN_WORKSPACE_ID && mainHasChanges}
                <span class="icon-[uil--exclamation-triangle] absolute inset-0 block size-4 text-warning" title="Uncommitted changes"></span>
              {:else if item.id !== MAIN_WORKSPACE_ID && configStore.worktreeIntegrateMode === 'pr' && item.pullRequest}
                {@const pr = item.pullRequest}
                {@const [iconClass, colorClass, title] =
                  pr.state === 'merged'
                    ? ['icon-[uil--code-branch] rotate-180', 'text-purple-400', `PR #${pr.number} merged`]
                    : pr.checkStatus === 'fail'
                      ? ['icon-[uil--times-circle]', 'text-danger', `PR #${pr.number}: checks failed`]
                      : pr.checkStatus === 'pending'
                        ? ['icon-[uil--clock]', 'text-warning', `PR #${pr.number}: checks running`]
                        : ['icon-[uil--check-circle]', 'text-success', `PR #${pr.number}: checks passed`]
                }
                <a href={pr.url} target="_blank" rel="noopener noreferrer" class={["absolute inset-0 block size-4 group-hover:hidden", iconClass, colorClass]} title={title}></a>
                <button
                  onclick={() => promptDestroy(item.id)}
                  class="absolute inset-0 hidden cursor-pointer items-center justify-center rounded text-text-faint transition-opacity hover:text-danger group-hover:flex"
                  aria-label="Destroy worktree"
                >
                  <span class="icon-[uil--archive] block size-3.5"></span>
                </button>
              {:else if item.id !== MAIN_WORKSPACE_ID && readyWorktreeIds.has(item.id)}
                <span class="icon-[uil--check] absolute inset-0 block size-4 text-success group-hover:hidden" title="Ready to integrate"></span>
                <button
                  onclick={() => promptDestroy(item.id)}
                  class="absolute inset-0 hidden cursor-pointer items-center justify-center rounded text-text-faint transition-opacity hover:text-danger group-hover:flex"
                  aria-label="Destroy worktree"
                >
                  <span class="icon-[uil--archive] block size-3.5"></span>
                </button>
              {:else if item.id !== MAIN_WORKSPACE_ID}
                <button
                  onclick={() => promptDestroy(item.id)}
                  class="absolute inset-0 flex cursor-pointer items-center justify-center rounded text-text-faint opacity-0 transition-opacity hover:text-danger group-hover:opacity-100"
                  aria-label="Destroy worktree"
                >
                  <span class="icon-[uil--archive] block size-3.5"></span>
                </button>
              {/if}
            </div>
          </div>

          {#if item.beans.length > 0}
            <div class="flex flex-col gap-0.5 px-3 pb-2">
              {#each item.beans as wtBean (wtBean.id)}
                <button
                  onclick={() => {
                    ui.selectBeanForView(wtBean.id, item.id);
                    ui.navigateTo(item.id);
                  }}
                  aria-label={wtBean.title}
                  class={[
                    'flex min-w-0 cursor-pointer items-baseline gap-1.5 rounded border-l-2 bg-surface-alt/50 px-2 py-1 text-left shadow-sm transition-colors hover:bg-surface-alt',
                    typeBorders[wtBean.type] ?? 'border-l-type-task-border'
                  ]}
                >
                  <span class="min-w-0 flex-1 text-xs text-text-muted" use:decryptText={{ text: wtBean.title, immediate: isBeanSeen(wtBean.id) }}></span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
      </div>
    {/if}
  </div>

  {/if}

  {#if confirmingRemoveId}
    {@const label = workspaceItems.find((w) => w.id === confirmingRemoveId)?.label ?? 'this worktree'}
    {@const warnings = [
      confirmingStatus?.hasChanges && 'uncommitted changes',
      confirmingStatus?.hasUnmergedCommits && 'unmerged commits'
    ].filter(Boolean)}
    {@const hooksNote = configStore.isWorktrunk
      ? " Worktrunk's pre-remove/post-remove hooks run, and the branch is deleted only if it is merged."
      : ''}
    <ConfirmModal
      title="Destroy Worktree"
      message={warnings.length > 0
        ? `This worktree has ${warnings.join(' and ')}. Are you sure you want to destroy the worktree for "${label}"?${hooksNote} This cannot be undone.`
        : `Are you sure you want to destroy the worktree for "${label}"?${hooksNote} This cannot be undone.`}
      confirmLabel="Destroy"
      danger
      onConfirm={() => handleRemoveWorktree(confirmingRemoveId!)}
      onCancel={() => { confirmingRemoveId = null; confirmingStatus = null; }}
    />
  {/if}

  {#if creatingWorkspace}
    <CreateWorkspaceModal
      onCreated={(wt) => {
        creatingWorkspace = false;
        ui.navigateTo(wt.id);
      }}
      onCancel={() => (creatingWorkspace = false)}
    />
  {/if}
</nav>
