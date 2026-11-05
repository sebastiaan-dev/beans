/**
 * Shared drag state for the backlog view.
 *
 * Since BeanItem is recursive, we need a single source of truth for
 * which bean is being dragged and where the drop target is. This module
 * provides that shared reactive state.
 *
 * Card hover uses three zones:
 *   - Top 25%:    reorder above
 *   - Middle 50%: reparent onto this bean
 *   - Bottom 25%: reorder below
 */

import type { Bean } from '$lib/beans.svelte';
import { beansStore } from '$lib/beans.svelte';
import { applyDrop, applyReparent } from '$lib/dragOrder';
import { isBacklogRoot } from '$lib/backlog';

export type DropMode = 'reorder' | 'reparent';

class BacklogDragState {
  draggedBeanId = $state<string | null>(null);
  /** The parent ID of the sibling group being hovered (null = top-level) */
  dropTargetParent = $state<string | null | undefined>(undefined);
  dropIndex = $state<number | null>(null);
  /** The bean ID being hovered for reparenting */
  reparentTargetId = $state<string | null>(null);
  dropMode = $state<DropMode>('reorder');
  /** The target status when dropping into a different section */
  targetStatus = $state<string | null>(null);

  get isDragging() {
    return this.draggedBeanId !== null;
  }

  startDrag(e: DragEvent, bean: Bean) {
    this.draggedBeanId = bean.id;
    e.dataTransfer!.effectAllowed = 'move';
    e.dataTransfer!.setData('text/plain', bean.id);
  }

  endDrag() {
    this.draggedBeanId = null;
    this.dropTargetParent = undefined;
    this.dropIndex = null;
    this.reparentTargetId = null;
    this.dropMode = 'reorder';
    this.targetStatus = null;
  }

  hoverCard(e: DragEvent, parentId: string | null, index: number, beanId: string, status?: string) {
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer!.dropEffect = 'move';

    // Don't allow dropping on yourself
    if (beanId === this.draggedBeanId) return;

    if (status) this.targetStatus = status;

    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const relativeY = (e.clientY - rect.top) / rect.height;

    if (relativeY < 0.25) {
      // Top zone — reorder above
      this.dropMode = 'reorder';
      this.dropTargetParent = parentId;
      this.dropIndex = index;
      this.reparentTargetId = null;
    } else if (relativeY > 0.75) {
      // Bottom zone — reorder below
      this.dropMode = 'reorder';
      this.dropTargetParent = parentId;
      this.dropIndex = index + 1;
      this.reparentTargetId = null;
    } else {
      // Middle zone — reparent onto this bean
      this.dropMode = 'reparent';
      this.reparentTargetId = beanId;
      this.dropTargetParent = undefined;
      this.dropIndex = null;
    }
  }

  hoverList(e: DragEvent, parentId: string | null, beanCount: number, status?: string) {
    e.preventDefault();
    e.dataTransfer!.dropEffect = 'move';
    this.dropMode = 'reorder';
    this.reparentTargetId = null;
    this.dropTargetParent = parentId;
    this.targetStatus = status ?? null;
    if (this.dropIndex === null || this.dropTargetParent !== parentId) {
      this.dropIndex = beanCount;
    }
  }

  leaveList(e: DragEvent, listEl: HTMLElement, parentId: string | null) {
    if (!listEl.contains(e.relatedTarget as Node)) {
      if (this.dropTargetParent === parentId) {
        this.dropTargetParent = undefined;
        this.dropIndex = null;
      }
      if (this.reparentTargetId) {
        this.reparentTargetId = null;
      }
    }
  }

  drop(e: DragEvent, parentId: string | null, beans: Bean[]) {
    e.preventDefault();
    e.stopPropagation();

    const mode = this.dropMode;
    const targetIdx = this.dropIndex;
    const beanId = this.draggedBeanId;
    const reparentTarget = this.reparentTargetId;
    const newStatus = this.targetStatus;

    this.dropTargetParent = undefined;
    this.dropIndex = null;
    this.draggedBeanId = null;
    this.reparentTargetId = null;
    this.dropMode = 'reorder';
    this.targetStatus = null;

    if (!beanId) return;

    if (mode === 'reparent' && reparentTarget) {
      const targetChildren = beansStore.children(reparentTarget);
      applyReparent(beanId, reparentTarget, targetChildren);
    } else {
      applyDrop(beans, beanId, targetIdx ?? beans.length, {
        newParentId: this.#dropParent(beanId, parentId),
        newStatus: newStatus ?? undefined
      });
    }
  }

  /**
   * Parent to assign when reordering into a sibling group. Beans whose parent is
   * outside the backlog are listed as section roots (see isBacklogRoot); moving
   * them around among the roots must not silently detach them from that parent.
   */
  #dropParent(beanId: string, parentId: string | null): string | null | undefined {
    if (parentId !== null) return parentId;
    const bean = beansStore.get(beanId);
    if (!bean?.parentId) return null;
    const visible = new Map(beansStore.all.map((b) => [b.id, b]));
    return isBacklogRoot(bean, visible) ? undefined : null;
  }

  /** Check if a drop indicator should show at this position */
  showIndicator(parentId: string | null, index: number, beanId: string, status?: string): boolean {
    return (
      this.dropMode === 'reorder' &&
      this.dropTargetParent === parentId &&
      (!status || this.targetStatus === status) &&
      this.draggedBeanId !== null &&
      this.draggedBeanId !== beanId &&
      this.dropIndex === index
    );
  }

  showEndIndicator(parentId: string | null, count: number, status?: string): boolean {
    return (
      this.dropMode === 'reorder' &&
      this.dropTargetParent === parentId &&
      (!status || this.targetStatus === status) &&
      this.draggedBeanId !== null &&
      this.dropIndex === count
    );
  }

  /** Check if this bean is the reparent target */
  isReparentTarget(beanId: string): boolean {
    return this.dropMode === 'reparent' && this.reparentTargetId === beanId;
  }
}

export const backlogDrag = new BacklogDragState();
