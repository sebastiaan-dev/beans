<script lang="ts">
  import { untrack } from 'svelte';
  import type { Snippet } from 'svelte';

  interface PanelDef {
    content: Snippet;
    size?: number;
    minSize?: number;
    maxSize?: number;
    collapsed?: boolean;
    persistKey?: string;
    /**
     * Render the panel at this size and disable resizing it, without forgetting
     * its regular (persisted) size — e.g. a sidebar collapsed to an icon rail.
     */
    fixedSize?: number;
  }

  interface Props {
    direction?: 'horizontal' | 'vertical';
    panels: PanelDef[];
  }

  let { direction = 'horizontal', panels }: Props = $props();

  const isHorizontal = $derived(direction === 'horizontal');
  const handlePx = 1;

  // The flex panel is the one without a fixed size
  const flexIndex = $derived(panels.findIndex((p) => p.size === undefined));

  // Initialize sizes from localStorage or panel defaults (runs once at creation)
  function initSizes(defs: PanelDef[]): number[] {
    return defs.map((p) => {
      if (p.size === undefined) return 0;
      if (p.persistKey) {
        const saved = localStorage.getItem(`beans-split-${p.persistKey}`);
        if (saved) {
          const parsed = parseInt(saved, 10);
          if (!Number.isNaN(parsed)) {
            return Math.max(p.minSize ?? 40, p.maxSize ? Math.min(parsed, p.maxSize) : parsed);
          }
        }
      }
      return p.size;
    });
  }

  let sizes = $state(untrack(() => initSizes(panels)));

  let isDragging = $state(false);
  let dragLeft = $state(-1);
  let dragRight = $state(-1);
  let dragStartMouse = $state(0);
  let dragStartSizes: number[] = [];
  let containerEl: HTMLDivElement | undefined = $state();

  function clampSize(idx: number, value: number): number {
    const panel = panels[idx];
    const min = panel.minSize ?? 40;
    const max = panel.maxSize;
    return Math.max(min, max ? Math.min(max, value) : value);
  }

  function startDrag(leftIdx: number, rightIdx: number, e: MouseEvent) {
    e.preventDefault();
    isDragging = true;
    dragLeft = leftIdx === flexIndex ? -1 : leftIdx;
    dragRight = rightIdx === flexIndex ? -1 : rightIdx;
    dragStartMouse = isHorizontal ? e.clientX : e.clientY;
    dragStartSizes = [...sizes];
  }

  function onDrag(e: MouseEvent) {
    if (!isDragging || !containerEl) return;
    const mousePos = isHorizontal ? e.clientX : e.clientY;
    const delta = mousePos - dragStartMouse;

    if (dragLeft >= 0) sizes[dragLeft] = clampSize(dragLeft, dragStartSizes[dragLeft] + delta);
    if (dragRight >= 0) sizes[dragRight] = clampSize(dragRight, dragStartSizes[dragRight] - delta);
  }

  function stopDrag() {
    if (isDragging) {
      isDragging = false;
      for (const idx of [dragLeft, dragRight]) {
        if (idx >= 0 && panels[idx]?.persistKey) {
          localStorage.setItem(`beans-split-${panels[idx].persistKey}`, sizes[idx].toString());
        }
      }
      dragLeft = -1;
      dragRight = -1;
    }
  }

  // Indices of non-collapsed panels, in order
  const visibleIndices = $derived(
    panels.reduce<number[]>((acc, p, i) => {
      if (!p.collapsed) acc.push(i);
      return acc;
    }, [])
  );
</script>

<svelte:window onmousemove={onDrag} onmouseup={stopDrag} />

<div
  bind:this={containerEl}
  class={['flex min-h-0 min-w-0 flex-1', isHorizontal ? 'flex-row' : 'flex-col']}
>
  {#each visibleIndices as panelIdx, vi (panelIdx)}
    <!-- Resize handle between consecutive visible panels -->
    {#if vi > 0 && (panels[visibleIndices[vi - 1]].fixedSize !== undefined || panels[panelIdx].fixedSize !== undefined)}
      <!-- Divider only: a fixed-size neighbour can't be resized -->
      <span class={['block shrink-0 bg-border', isHorizontal ? 'w-px' : 'h-px']}></span>
    {:else if vi > 0}
      <button
        class={[
          'group relative z-10 shrink-0 border-0 rounded-none bg-transparent p-0',
          isHorizontal
            ? 'flex w-3 -mx-[5.5px] cursor-col-resize justify-center'
            : 'flex h-3 -my-[5.5px] cursor-row-resize items-center'
        ]}
        aria-label="Resize"
        onmousedown={(e) => startDrag(visibleIndices[vi - 1], panelIdx, e)}
      >
        <span
          class={[
            'block transition-colors',
            isHorizontal ? 'h-full w-px' : 'w-full h-px',
            isDragging ? 'bg-surface-dim' : 'bg-border group-hover:bg-surface-dim'
          ]}
        ></span>
      </button>
    {/if}

    <!-- Panel content -->
    {#if panels[panelIdx].size === undefined}
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        {@render panels[panelIdx].content()}
      </div>
    {:else}
      <div
        class="flex shrink-0 flex-col overflow-hidden"
        style="{isHorizontal ? 'width' : 'height'}: {panels[panelIdx].fixedSize ?? sizes[panelIdx]}px"
      >
        {@render panels[panelIdx].content()}
      </div>
    {/if}
  {/each}
</div>
