<script lang="ts">
  import { fly } from 'svelte/transition';
  import { toasts } from '$lib/toasts.svelte';
  import CommandMessage from './CommandMessage.svelte';

  const styles = {
    info: { box: 'border-border', icon: 'icon-[uil--info-circle] text-text-muted' },
    success: { box: 'border-success/40', icon: 'icon-[uil--check-circle] text-success' },
    error: { box: 'border-danger/40', icon: 'icon-[uil--exclamation-circle] text-danger' }
  } as const;
</script>

<div
  class="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-full max-w-md flex-col gap-2"
  aria-live="polite"
>
  {#each toasts.toasts as toast (toast.id)}
    <div
      class={[
        'pointer-events-auto flex items-start gap-2 rounded-lg border bg-surface px-3 py-2.5 shadow-lg',
        styles[toast.kind].box
      ]}
      role={toast.kind === 'error' ? 'alert' : 'status'}
      data-toast={toast.kind}
      transition:fly={{ y: 12, duration: 150 }}
    >
      <span class={['mt-0.5 block size-4 shrink-0', styles[toast.kind].icon]}></span>
      <div class="min-w-0 flex-1">
        <p class="text-sm font-medium text-text">{toast.title}</p>
        {#if toast.message}
          <CommandMessage
            message={toast.message}
            class="mt-1 max-h-64 overflow-y-auto text-text-muted"
          />
        {/if}
      </div>
      <button
        class="shrink-0 cursor-pointer px-1 text-xs text-text-faint hover:text-text"
        aria-label="Dismiss notification"
        onclick={() => toasts.dismiss(toast.id)}
      >
        ✕
      </button>
    </div>
  {/each}
</div>
