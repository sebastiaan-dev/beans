<script lang="ts">
  import type { Bean } from '$lib/beans.svelte';
  import { beansStore } from '$lib/beans.svelte';
  import { configStore } from '$lib/config.svelte';
  import { client } from '$lib/graphqlClient';
  import { CreateBeanDocument, UpdateBeanDocument, type CreateBeanInput, type UpdateBeanInput } from '$lib/graphql/generated';

  interface Props {
    bean?: Bean | null;
    onClose: () => void;
    onSaved?: (bean: Bean) => void;
  }

  let { bean = null, onClose, onSaved }: Props = $props();

  const isEdit = $derived(!!bean);

  // Form fields — intentionally capture initial prop values for local editing
  /* eslint-disable svelte/valid-compile */
  // svelte-ignore state_referenced_locally
  let title = $state(bean?.title ?? '');
  // Type and status follow the configured defaults (beans.default_type /
  // default_status) until the user picks a value, so new beans show — and are
  // created with — the project's real defaults even if config loads late.
  // svelte-ignore state_referenced_locally
  let typeChoice = $state<string | null>(bean?.type ?? null);
  // svelte-ignore state_referenced_locally
  let statusChoice = $state<string | null>(bean?.status ?? null);
  const type = $derived(typeChoice ?? configStore.defaultBeanType);
  const status = $derived(statusChoice ?? configStore.defaultBeanStatus);
  // svelte-ignore state_referenced_locally
  let priority = $state(bean?.priority ?? 'normal');
  // svelte-ignore state_referenced_locally
  let tags = $state(bean?.tags.join(', ') ?? '');
  // svelte-ignore state_referenced_locally
  let body = $state(bean?.body ?? '');
  // svelte-ignore state_referenced_locally
  let parentId = $state(bean?.parentId ?? '');
  /* eslint-enable svelte/valid-compile */

  let submitting = $state(false);
  let error = $state<string | null>(null);

  const types = ['task', 'bug', 'feature', 'epic', 'milestone'];
  const statuses = ['draft', 'todo', 'in-progress', 'completed', 'scrapped'];
  const priorities = ['critical', 'high', 'normal', 'low', 'deferred'];

  // Available parents (all beans except current bean and its descendants)
  const availableParents = $derived(
    beansStore.all.filter((b) => {
      if (!bean) return true;
      if (b.id === bean.id) return false;
      // Simple cycle check: don't allow own children as parent
      let current: Bean | undefined = b;
      while (current) {
        if (current.parentId === bean.id) return false;
        current = current.parentId ? beansStore.get(current.parentId) : undefined;
      }
      return true;
    })
  );

  function parseTags(raw: string): string[] {
    return raw
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean);
  }

  async function handleSubmit() {
    if (!title.trim()) {
      error = 'Title is required';
      return;
    }

    submitting = true;
    error = null;

    const fields = {
      title: title.trim(),
      type,
      status,
      priority,
      body: body || null,
      tags: parseTags(tags),
      parent: parentId || null
    };

    let saved: Bean | null = null;
    if (isEdit && bean) {
      const input: UpdateBeanInput = fields;
      const result = await client.mutation(UpdateBeanDocument, { id: bean.id, input }).toPromise();
      submitting = false;
      if (result.error) { error = result.error.message; return; }
      saved = result.data?.updateBean ?? null;
    } else {
      // Leave untouched type/status to the server, which applies the
      // configured defaults: the form may be submitted before the config
      // (and thus configStore's defaults) has loaded.
      const input: CreateBeanInput = {
        ...fields,
        type: typeChoice ?? undefined,
        status: statusChoice ?? undefined
      };
      const result = await client.mutation(CreateBeanDocument, { input }).toPromise();
      submitting = false;
      if (result.error) { error = result.error.message; return; }
      saved = result.data?.createBean ?? null;
    }
    if (saved) {
      onSaved?.(saved);
    }
    onClose();
  }
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
  <div class="w-11/12 max-w-2xl rounded-xl bg-surface p-6 shadow-xl">
    <h3 class="text-lg font-bold text-text">{isEdit ? 'Edit Bean' : 'New Bean'}</h3>

    {#if error}
      <div
        class="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
      >
        {error}
      </div>
    {/if}

    <form
      onsubmit={(e) => {
        e.preventDefault();
        handleSubmit();
      }}
      class="mt-4 space-y-4"
    >
      <!-- Title -->
      <div>
        <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-title">Title</label>
        <input
          id="bean-title"
          type="text"
          class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
          bind:value={title}
          placeholder="What needs to be done?"
        />
      </div>

      <!-- Type / Status / Priority row -->
      <div class="grid grid-cols-3 gap-3">
        <div>
          <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-type">Type</label>
          <select
            id="bean-type"
            class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
            bind:value={() => type, (v) => (typeChoice = v)}
          >
            {#each types as t}
              <option value={t}>{t}</option>
            {/each}
          </select>
        </div>

        <div>
          <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-status"
            >Status</label
          >
          <select
            id="bean-status"
            class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
            bind:value={() => status, (v) => (statusChoice = v)}
          >
            {#each statuses as s}
              <option value={s}>{s}</option>
            {/each}
          </select>
        </div>

        <div>
          <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-priority"
            >Priority</label
          >
          <select
            id="bean-priority"
            class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
            bind:value={priority}
          >
            {#each priorities as p}
              <option value={p}>{p}</option>
            {/each}
          </select>
        </div>
      </div>

      <!-- Parent -->
      <div>
        <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-parent"
          >Parent</label
        >
        <select
          id="bean-parent"
          class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
          bind:value={parentId}
        >
          <option value="">None</option>
          {#each availableParents as p}
            <option value={p.id}>{p.title} ({p.type})</option>
          {/each}
        </select>
      </div>

      <!-- Tags -->
      <div>
        <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-tags">Tags</label>
        <input
          id="bean-tags"
          type="text"
          class="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
          bind:value={tags}
          placeholder="Comma-separated tags"
        />
      </div>

      <!-- Body -->
      <div>
        <label class="mb-1 block text-sm font-medium text-text-muted" for="bean-body"
          >Description (Markdown)</label
        >
        <textarea
          id="bean-body"
          class="h-40 w-full resize-y rounded-md border border-border bg-surface px-3 py-2 font-mono text-sm text-text focus:border-accent focus:ring-2 focus:ring-accent/50 focus:outline-none"
          bind:value={body}
          placeholder="Markdown content..."
        ></textarea>
      </div>

      <!-- Actions -->
      <div class="flex justify-end gap-2 pt-2">
        <button
          type="button"
          class="cursor-pointer rounded-md border border-border px-4 py-2 text-sm font-medium text-text-muted transition-colors hover:bg-surface-alt"
          onclick={onClose}
          disabled={submitting}>Cancel</button
        >
        <button
          type="submit"
          class="flex cursor-pointer items-center gap-2 rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-text transition-opacity hover:opacity-90 disabled:opacity-50"
          disabled={submitting || !title.trim()}
        >
          {#if submitting}
            <span
              class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-accent-text/30 border-t-accent-text"
            ></span>
          {/if}
          {isEdit ? 'Save Changes' : 'Create Bean'}
        </button>
      </div>
    </form>
  </div>
  <!-- Backdrop -->
  <button class="fixed inset-0 -z-10" onclick={onClose} tabindex="-1" aria-label="Close"></button>
</div>
