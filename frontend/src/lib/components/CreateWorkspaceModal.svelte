<script lang="ts">
  import { onMount } from 'svelte';
  import { configStore } from '$lib/config.svelte';
  import { generateWorkspaceName } from '$lib/nameGenerator';
  import { validateWorkspaceName, workspaceLabel } from '$lib/workspaces';
  import { worktreeStore, type Worktree } from '$lib/worktrees.svelte';
  import CommandMessage from './CommandMessage.svelte';

  interface Props {
    onCreated: (wt: Worktree) => void;
    onCancel: () => void;
  }

  let { onCreated, onCancel }: Props = $props();

  let name = $state(generateWorkspaceName());
  let submitting = $state(false);
  let serverError = $state<string | null>(null);
  let input: HTMLInputElement | undefined = $state();

  const validation = $derived(validateWorkspaceName(name));
  /** Client-side conflicts with workspaces we already know about. */
  const conflict = $derived.by(() => {
    if (validation) return null;
    const sameId = worktreeStore.worktrees.find((wt) => wt.id === name);
    if (sameId) return `A workspace named "${workspaceLabel(sameId)}" already exists.`;
    const sameBranch = worktreeStore.worktrees.find((wt) => wt.branch === name);
    if (sameBranch) {
      return `Branch "${name}" is already checked out in workspace "${workspaceLabel(sameBranch)}".`;
    }
    return null;
  });
  const canSubmit = $derived(!validation && !conflict && !submitting);

  onMount(() => {
    input?.focus();
    input?.select();
  });

  async function submit(e?: Event) {
    e?.preventDefault();
    if (!canSubmit) return;
    submitting = true;
    serverError = null;
    const wt = await worktreeStore.createWorktree({ name });
    submitting = false;
    if (wt) {
      onCreated(wt);
    } else {
      serverError = worktreeStore.error ?? 'Failed to create the workspace.';
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && !submitting) onCancel();
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget && !submitting) onCancel();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
  onclick={handleBackdropClick}
>
  <div
    class="mx-4 w-full max-w-md rounded-lg border border-border bg-surface p-5 shadow-xl"
    role="dialog"
    aria-modal="true"
    aria-labelledby="create-workspace-title"
  >
    <form onsubmit={submit}>
      <h2 id="create-workspace-title" class="mb-3 text-base font-semibold text-text">
        New workspace
      </h2>

      <label class="mb-1 block text-xs font-medium text-text-muted" for="workspace-name">
        Name (git branch)
      </label>
      <input
        id="workspace-name"
        bind:this={input}
        bind:value={name}
        class={[
          'w-full rounded-md border bg-surface-alt px-2.5 py-1.5 font-mono text-sm text-text outline-none focus:ring-1',
          validation || conflict
            ? 'border-danger/60 focus:ring-danger/40'
            : 'border-border focus:border-accent/60 focus:ring-accent/30'
        ]}
        autocomplete="off"
        spellcheck="false"
        maxlength={200}
        aria-invalid={!!(validation || conflict)}
        aria-describedby="workspace-name-help"
        disabled={submitting}
      />

      <div id="workspace-name-help" class="mt-1.5 min-h-5 text-xs">
        {#if validation}
          <p class="text-danger" role="alert" data-name-error>
            Invalid name: {validation.reason}.
            {#if validation.suggestion}
              <button
                type="button"
                class="ml-1 cursor-pointer font-mono underline hover:no-underline"
                onclick={() => {
                  name = validation!.suggestion!;
                  input?.focus();
                }}
              >
                Use "{validation.suggestion}"
              </button>
            {/if}
          </p>
        {:else if conflict}
          <p class="text-danger" role="alert" data-name-error>{conflict}</p>
        {:else}
          <p class="text-text-faint">
            Creates branch <code class="font-mono text-text-muted" data-branch-preview>{name}</code>
            from <code class="font-mono text-text-muted">{configStore.worktreeBaseRef}</code>.
          </p>
        {/if}
      </div>

      {#if configStore.isWorktrunk}
        <p class="mt-2 text-xs text-text-faint">
          Worktrunk places the worktree using its <code class="font-mono">worktree-path</code>
          template and runs the project's <code class="font-mono">pre-start</code>/<code
            class="font-mono">post-start</code
          >
          hooks as workspace setup.
        </p>
      {/if}

      {#if serverError}
        <div
          class="mt-3 rounded-md border border-danger/30 bg-danger/5 p-2.5"
          role="alert"
          data-create-error
        >
          <p class="mb-1 text-xs font-semibold text-danger">Could not create the workspace</p>
          <CommandMessage message={serverError} class="max-h-48 overflow-y-auto text-danger/80" />
        </div>
      {/if}

      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          class="cursor-pointer rounded-md border border-border px-3 py-1.5 text-sm font-medium text-text-muted transition-colors hover:bg-surface-alt"
          onclick={onCancel}
          disabled={submitting}
        >
          Cancel
        </button>
        <button
          type="submit"
          class={[
            'rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-accent-text transition-opacity',
            canSubmit ? 'cursor-pointer hover:opacity-90' : 'cursor-not-allowed opacity-50'
          ]}
          disabled={!canSubmit}
        >
          {submitting ? 'Creating…' : 'Create'}
        </button>
      </div>
    </form>
  </div>
</div>
