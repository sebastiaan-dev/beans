/**
 * Shared drag-and-drop ordering utilities.
 *
 * Used by both BoardView (kanban columns) and backlog (flat/hierarchical list)
 * to compute fractional-index order keys when beans are reordered via drag.
 */

import type { Bean } from '$lib/beans.svelte';
import { beansStore } from '$lib/beans.svelte';
import { orderBetween } from '$lib/fractional';
import { client } from '$lib/graphqlClient';
import { UpdateBeanOrderDocument } from './graphql/generated';

/**
 * Ensure all beans in the list have order keys.
 * Assigns evenly-spaced keys to any beans missing them,
 * preserving the relative positions of beans that already have keys.
 * Returns the list with orders filled in. Updates the store optimistically.
 */
export function ensureOrdered(beans: Bean[]): Bean[] {
  const needsOrder = beans.filter((b) => !b.order);
  if (needsOrder.length === 0) return beans;

  const result = [...beans];
  let key = '';
  for (let i = 0; i < result.length; i++) {
    const nextKey = i < result.length - 1 && result[i + 1].order ? result[i + 1].order : '';
    if (!result[i].order) {
      const newOrder = orderBetween(key, nextKey);
      result[i] = { ...result[i], order: newOrder };
      beansStore.optimisticUpdate(result[i].id, { order: newOrder });
      client
        .mutation(UpdateBeanOrderDocument, { id: result[i].id, input: { order: newOrder } })
        .toPromise();
    }
    key = result[i].order;
  }
  return result;
}

/**
 * Compute the fractional-index order key for a bean being dropped
 * at `targetIndex` within `beans`. The dragged bean (identified by
 * `draggedId`) is filtered out before computing neighbours.
 */
export function computeOrder(beans: Bean[], targetIndex: number, draggedId: string): string {
  const draggedIndex = beans.findIndex((b) => b.id === draggedId);
  const filtered = beans.filter((b) => b.id !== draggedId);

  if (filtered.length === 0) {
    return orderBetween('', '');
  }

  // Adjust target index when dragging downward in the same list
  let idx = targetIndex;
  if (draggedIndex >= 0 && targetIndex > draggedIndex) {
    idx--;
  }
  idx = Math.min(idx, filtered.length);

  if (idx === 0) {
    return orderBetween('', filtered[0].order);
  }
  if (idx >= filtered.length) {
    return orderBetween(filtered[filtered.length - 1].order, '');
  }

  return orderBetween(filtered[idx - 1].order, filtered[idx].order);
}

/**
 * Reparent a bean: make it a child of newParentId (or top-level if null),
 * placing it at the end of the new parent's children.
 */
/** Valid parent types per bean type (must match backend's ValidParentTypes) */
const VALID_PARENT_TYPES: Record<string, string[]> = {
  milestone: [],
  epic: ['milestone'],
  feature: ['milestone', 'epic'],
  task: ['milestone', 'epic', 'feature'],
  bug: ['milestone', 'epic', 'feature']
};

export function applyReparent(
  draggedId: string,
  newParentId: string | null,
  targetChildren: Bean[]
): void {
  const bean = beansStore.get(draggedId);
  if (!bean) return;

  // Don't reparent to self or to current parent
  if (newParentId === draggedId) return;
  if (bean.parentId === newParentId) return;

  // Prevent creating cycles: newParentId must not be a descendant of draggedId
  if (newParentId && isDescendant(newParentId, draggedId)) return;

  // Validate type hierarchy client-side
  if (newParentId) {
    const parent = beansStore.get(newParentId);
    if (!parent) return;
    const validTypes = VALID_PARENT_TYPES[bean.type] ?? ['milestone', 'epic', 'feature'];
    if (!validTypes.includes(parent.type)) return;
  }

  // Compute order key at end of new sibling list
  const orderedChildren = ensureOrdered(targetChildren);
  const newOrder =
    orderedChildren.length > 0
      ? orderBetween(orderedChildren[orderedChildren.length - 1].order, '')
      : orderBetween('', '');

  // Save previous state for rollback
  const prevParentId = bean.parentId;
  const prevOrder = bean.order;

  beansStore.optimisticUpdate(draggedId, { parentId: newParentId, order: newOrder });

  const input: Record<string, string | null> = { parent: newParentId ?? '', order: newOrder };
  client
    .mutation(UpdateBeanOrderDocument, { id: draggedId, input })
    .toPromise()
    .then((result) => {
      if (result.error) {
        console.error('Failed to reparent bean:', result.error);
        // Roll back optimistic update
        beansStore.optimisticUpdate(draggedId, { parentId: prevParentId, order: prevOrder });
      }
    });
}

/** Check if candidateId is a descendant of ancestorId */
function isDescendant(candidateId: string, ancestorId: string): boolean {
  let current = beansStore.get(candidateId);
  while (current?.parentId) {
    if (current.parentId === ancestorId) return true;
    current = beansStore.get(current.parentId);
  }
  return false;
}

/**
 * Apply a drop: compute the new order, optimistically update the store,
 * and fire the GraphQL mutation. Optionally changes status (board) or
 * parent (backlog cross-group reorder).
 */
export function applyDrop(
  beans: Bean[],
  draggedId: string,
  targetIndex: number,
  opts?: { newStatus?: string; newParentId?: string | null }
): void {
  const bean = beansStore.get(draggedId);
  if (!bean) return;

  const newStatus = opts?.newStatus;
  // undefined = don't change parent; null = move to top-level; string = reparent
  const newParentId = opts?.newParentId;
  const changingParent = newParentId !== undefined && bean.parentId !== newParentId;

  // Validate type hierarchy if reparenting
  if (changingParent && newParentId) {
    const parent = beansStore.get(newParentId);
    if (!parent) return;
    const validTypes = VALID_PARENT_TYPES[bean.type] ?? ['milestone', 'epic', 'feature'];
    if (!validTypes.includes(parent.type)) return;
  }

  const orderedBeans = ensureOrdered(beans);
  const newOrder = computeOrder(orderedBeans, targetIndex, draggedId);

  const sameStatus = !newStatus || bean.status === newStatus;
  if (sameStatus && !changingParent && bean.order === newOrder) return;

  // Save previous state for rollback
  const prevOrder = bean.order;
  const prevStatus = bean.status;
  const prevParentId = bean.parentId;

  const optimistic: Partial<Bean> = { order: newOrder };
  if (!sameStatus) optimistic.status = newStatus;
  if (changingParent) optimistic.parentId = newParentId;
  beansStore.optimisticUpdate(draggedId, optimistic);

  const input: Record<string, string | null> = { order: newOrder };
  if (!sameStatus) input.status = newStatus!;
  if (changingParent) input.parent = newParentId ?? '';
  client
    .mutation(UpdateBeanOrderDocument, { id: draggedId, input })
    .toPromise()
    .then((result) => {
      if (result.error) {
        console.error('Failed to update bean:', result.error);
        beansStore.optimisticUpdate(draggedId, {
          order: prevOrder,
          status: prevStatus,
          parentId: prevParentId
        });
      }
    });
}
