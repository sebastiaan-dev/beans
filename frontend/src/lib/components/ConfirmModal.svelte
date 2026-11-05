<script lang="ts">
  interface Props {
    title: string;
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
    /** Disable the confirm button (e.g. while required information is loading). */
    confirmDisabled?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  }

  let {
    title,
    message,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    danger = false,
    confirmDisabled = false,
    onConfirm,
    onCancel
  }: Props = $props();

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onCancel();
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) onCancel();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
  onclick={handleBackdropClick}
>
  <div class="mx-4 w-full max-w-sm rounded-lg border border-border bg-surface p-5 shadow-xl">
    <h2 class="mb-2 text-base font-semibold text-text">{title}</h2>
    <p class="mb-5 text-sm text-text-muted">{message}</p>
    <div class="flex justify-end gap-2">
      <button
        class="cursor-pointer rounded-md border border-border px-3 py-1.5 text-sm font-medium text-text-muted transition-colors hover:bg-surface-alt"
        onclick={onCancel}
      >
        {cancelLabel}
      </button>
      <button
        class={[
          'cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50',
          danger
            ? 'bg-danger text-white hover:opacity-90'
            : 'bg-accent text-accent-text hover:opacity-90'
        ]}
        disabled={confirmDisabled}
        onclick={onConfirm}
      >
        {confirmLabel}
      </button>
    </div>
  </div>
</div>
