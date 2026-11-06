import { beansStore, sortBeans, type Bean } from '$lib/beans.svelte';
import { BACKLOG_STATUSES, backlogRoots } from '$lib/backlog';
import { ui } from '$lib/uiState.svelte';
import {
  compileFilter,
  createFilterContext,
  filterTree,
  formatFilter,
  isArchived,
  isEmptyFilter,
  mentionsArchived,
  parseFilter,
  type FilterQuery,
  type SuggestionSources
} from '$lib/filter';

type BacklogStatus = (typeof BACKLOG_STATUSES)[number];

/** Board columns, left to right. */
export const BOARD_STATUSES = ['todo', 'in-progress', 'completed'] as const;
export type BoardStatus = (typeof BOARD_STATUSES)[number];

function containerRank(type: string): number {
  return type === 'milestone' ? 0 : type === 'epic' ? 1 : 2;
}

/**
 * Reactive view of the planning filter (`ui.filterText`) shared by the filter
 * input, popover, chips, backlog and board. The query string stays the single
 * source of truth; everything here is derived from it.
 */
class PlanningFilter {
  readonly query: FilterQuery = $derived(parseFilter(ui.filterText));
  readonly active = $derived(!isEmptyFilter(this.query));
  readonly context = $derived(createFilterContext(beansStore.beans.values()));
  readonly predicate = $derived(compileFilter(this.query, this.context));

  /**
   * Whether archived beans are candidates: only when the query has an
   * archived: clause (e.g. `archived:yes`); otherwise they stay hidden.
   */
  readonly includesArchived = $derived(mentionsArchived(this.query));

  /** Beans the planning views filter (sorted): non-archived ones, plus archived ones if requested. */
  readonly beans: Bean[] = $derived(
    this.includesArchived ? beansStore.allWithArchived : beansStore.all
  );

  /** Archived beans that match the filter (empty unless the query asks for archived beans). */
  readonly archivedMatches: Bean[] = $derived(
    this.includesArchived ? this.beans.filter((b) => isArchived(b) && this.predicate(b)) : []
  );

  /**
   * Matched beans plus their ancestors among the visible beans, or null when
   * unfiltered. Archived matches are included without ancestors: the bean tree
   * only nests non-archived beans, so they are rendered as roots.
   */
  readonly tree = $derived.by(() => {
    if (!this.active) return null;
    const tree = filterTree(beansStore.all, this.predicate, this.context.byId);
    for (const bean of this.archivedMatches) {
      tree.matched.add(bean.id);
      tree.visible.add(bean.id);
    }
    return tree;
  });

  /**
   * Root beans of each backlog section, in display order: top-level backlog
   * beans plus backlog beans whose ancestors are not in the backlog (e.g. todo
   * children of an in-progress parent). Archived beans requested by the filter
   * (archived:yes) are never nested (BeanItem only renders non-archived
   * children), so they are extra roots. With a filter, only roots that match
   * or have a matching descendant are kept.
   */
  readonly backlogSections: Record<BacklogStatus, Bean[]> = $derived.by(() => {
    const tree = this.tree;
    const sections = {} as Record<BacklogStatus, Bean[]>;
    for (const status of BACKLOG_STATUSES) {
      const roots = [
        ...backlogRoots(beansStore.all, status),
        ...this.archivedMatches.filter((b) => b.status === status)
      ];
      sections[status] = tree ? roots.filter((b) => tree.visible.has(b.id)) : roots;
    }
    return sections;
  });

  /** Beans of each board column, in display order (scrapped beans never show on the board). */
  readonly boardColumns: Record<BoardStatus, Bean[]> = $derived.by(() => {
    const columns = {} as Record<BoardStatus, Bean[]>;
    for (const status of BOARD_STATUSES) {
      // sortBeans handles order → priority → type → title sorting
      columns[status] = sortBeans(
        this.beans.filter((b) => b.status === status && this.predicate(b))
      );
    }
    return columns;
  });

  /** Values offered by autocomplete and the popover. */
  readonly sources: SuggestionSources = $derived.by(() => {
    const all = beansStore.all;
    const tags = new Set(all.flatMap((b) => b.tags.map((t) => t.toLowerCase())));
    const parentIds = new Set(all.map((b) => b.parentId).filter(Boolean));
    return {
      tags: [...tags].sort(),
      // Containers first: milestones/epics, then any other bean that has children.
      beans: all
        .filter((b) => b.type === 'milestone' || b.type === 'epic' || parentIds.has(b.id))
        .toSorted((a, b) => containerRank(a.type) - containerRank(b.type))
    };
  });

  /** Apply a structural edit and write the canonical query back. */
  update(edit: (query: FilterQuery) => FilterQuery) {
    ui.setFilterText(formatFilter(edit(this.query)));
  }
}

export const planningFilter = new PlanningFilter();
