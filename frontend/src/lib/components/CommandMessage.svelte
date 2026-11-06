<script lang="ts">
  import { parseMessageSegments } from '$lib/messageSegments';

  interface Props {
    /** Message text; `backticked` commands get a copy button. */
    message: string;
    class?: string;
  }

  let { message, class: className = '' }: Props = $props();

  const segments = $derived(parseMessageSegments(message));

  let copiedIndex = $state<number | null>(null);
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;

  async function copy(text: string, index: number) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      return;
    }
    copiedIndex = index;
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => (copiedIndex = null), 1500);
  }
</script>

<div class={['text-xs leading-relaxed break-words whitespace-pre-wrap', className]} data-command-message>
  {#each segments as seg, i (i)}
    {#if seg.kind === 'text'}{seg.text}{:else}<span
        class="inline-flex max-w-full items-center gap-1 rounded border border-border bg-surface-alt px-1 align-baseline font-mono text-[11px] text-text whitespace-normal"
        ><code class="break-all">{seg.command}</code><button
          type="button"
          class="inline-flex shrink-0 cursor-pointer items-center text-text-faint hover:text-text"
          title={`Copy: ${seg.copyText}`}
          aria-label={`Copy command: ${seg.copyText}`}
          data-copy-command={seg.copyText}
          onclick={() => copy(seg.copyText, i)}
          >{#if copiedIndex === i}<span class="icon-[uil--check] size-3.5 text-success"></span>{:else}<span
              class="icon-[uil--copy] size-3.5"
            ></span>{/if}</button
        ></span
      >{/if}
  {/each}
</div>
