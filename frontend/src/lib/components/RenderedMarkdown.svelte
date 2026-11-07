<script lang="ts">
  import { beansStore, type Bean } from '$lib/beans.svelte';
  import { ui } from '$lib/uiState.svelte';
  import { renderMarkdown, updateBeanIdMatcher, type BeanIdMatcher } from '$lib/markdown';

  interface Props {
    content: string;
    class?: string;
    /** Called when a bean link in the content is clicked (default: select the bean). */
    onBeanLink?: (bean: Bean) => void;
  }

  let { content, class: className, onBeanLink }: Props = $props();

  let renderedHtml = $state('');

  // Link the IDs of existing beans; re-render when beans are added or removed.
  let lastBeanIds: BeanIdMatcher | undefined;
  const beanIds = $derived.by(
    () => (lastBeanIds = updateBeanIdMatcher(lastBeanIds, beansStore.beans.keys()))
  );

  $effect(() => {
    if (content) {
      renderMarkdown(content, { beanIds }).then((html) => {
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
