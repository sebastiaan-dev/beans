<script lang="ts">
  import { beansStore, type Bean } from '$lib/beans.svelte';
  import { ui } from '$lib/uiState.svelte';
  import { renderMarkdown } from '$lib/markdown';

  interface Props {
    content: string;
    class?: string;
    /** Called when a bean link in the content is clicked (default: select the bean). */
    onBeanLink?: (bean: Bean) => void;
  }

  let { content, class: className, onBeanLink }: Props = $props();

  let renderedHtml = $state('');

  $effect(() => {
    if (content) {
      renderMarkdown(content).then((html) => {
        renderedHtml = html;
      });
    } else {
      renderedHtml = '';
    }
  });

  function handleBeanLinkClick(e: MouseEvent) {
    const target = (e.target as HTMLElement).closest<HTMLElement>('[data-bean-id]');
    if (!target) return;
    e.preventDefault();
    const linkedBean = beansStore.get(target.dataset.beanId!);
    if (!linkedBean) return;
    if (onBeanLink) onBeanLink(linkedBean);
    else ui.selectBean(linkedBean);
  }
</script>

{#if renderedHtml}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class={className} onclick={handleBeanLinkClick}>
    {@html renderedHtml}
  </div>
{/if}
