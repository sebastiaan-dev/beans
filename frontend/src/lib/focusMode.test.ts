import { describe, expect, it } from 'vitest';
import type { Bean } from '$lib/beans.svelte';
import {
  backlogOrder,
  focusActionForKey,
  focusIdFromUrl,
  focusPosition,
  focusSourceFromPath,
  hasRelated,
  isFocusEnterShortcut,
  isTypingTarget,
  relativeHref,
  relatedBeans,
  withFocus
} from './focusMode';

function bean(id: string, fields: Partial<Bean> = {}): Bean {
  return {
    id,
    title: id,
    status: 'todo',
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...fields
  } as Bean;
}

function key(key: string, mods: Partial<KeyboardEventInit> & { target?: unknown } = {}) {
  return {
    key,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...mods
  } as Parameters<typeof focusActionForKey>[0];
}

/** Minimal stand-in for a DOM element (vitest runs these tests in node). */
function el(
  tagName: string,
  opts: { type?: string; isContentEditable?: boolean; inside?: string[] } = {}
) {
  return {
    tagName,
    type: opts.type,
    isContentEditable: opts.isContentEditable ?? false,
    closest: (selector: string) =>
      (opts.inside ?? []).some((s) => selector.split(', ').includes(s)) ? {} : null
  } as unknown as EventTarget;
}

describe('focus URL', () => {
  it('reads the focused bean from ?focus=', () => {
    expect(focusIdFromUrl(new URL('http://x/planning?focus=beans-1&q=tag:ui'))).toBe('beans-1');
    expect(focusIdFromUrl(new URL('http://x/planning'))).toBeNull();
    expect(focusIdFromUrl(new URL('http://x/planning?focus='))).toBeNull();
    expect(focusIdFromUrl(new URL('http://x/planning?focus=%20'))).toBeNull();
  });

  it('sets and clears the param, keeping the others', () => {
    const url = new URL('http://x/planning/board?q=tag%3Aui&bean=a#h');
    const focused = withFocus(url, 'beans-2');
    expect(relativeHref(focused)).toBe('/planning/board?q=tag%3Aui&bean=a&focus=beans-2#h');
    expect(relativeHref(withFocus(focused, null))).toBe('/planning/board?q=tag%3Aui&bean=a#h');
    // The input is not modified
    expect(url.searchParams.has('focus')).toBe(false);
  });

  it('derives the source list from the path', () => {
    expect(focusSourceFromPath('/planning')).toBe('backlog');
    expect(focusSourceFromPath('/planning/board')).toBe('board');
    expect(focusSourceFromPath('/workspace/foo')).toBe('none');
  });
});

describe('keyboard', () => {
  it('detects typing targets', () => {
    expect(isTypingTarget(el('INPUT'))).toBe(true);
    expect(isTypingTarget(el('INPUT', { type: 'search' }))).toBe(true);
    expect(isTypingTarget(el('INPUT', { type: 'checkbox' }))).toBe(false);
    expect(isTypingTarget(el('TEXTAREA'))).toBe(true);
    expect(isTypingTarget(el('SELECT'))).toBe(true);
    expect(isTypingTarget(el('DIV', { isContentEditable: true }))).toBe(true);
    expect(isTypingTarget(el('DIV', { inside: ['.xterm'] }))).toBe(true);
    expect(isTypingTarget(el('BUTTON'))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });

  it('enters with a plain f only', () => {
    expect(isFocusEnterShortcut(key('f'))).toBe(true);
    expect(isFocusEnterShortcut(key('f', { metaKey: true }))).toBe(false); // Cmd+F: filter
    expect(isFocusEnterShortcut(key('f', { ctrlKey: true }))).toBe(false);
    expect(isFocusEnterShortcut(key('F', { shiftKey: true }))).toBe(false);
    expect(isFocusEnterShortcut(key('f', { target: el('INPUT') }))).toBe(false);
    expect(isFocusEnterShortcut({ ...key('f'), defaultPrevented: true })).toBe(false);
    expect(isFocusEnterShortcut({ ...key('f'), isComposing: true })).toBe(false);
    expect(isFocusEnterShortcut(key('g'))).toBe(false);
  });

  const cases: { name: string; event: ReturnType<typeof key>; want: string | null }[] = [
    { name: 'Esc exits', event: key('Escape'), want: 'exit' },
    {
      name: 'Esc in an input does not exit',
      event: key('Escape', { target: el('INPUT') }),
      want: null
    },
    { name: 'Shift+Esc is ignored', event: key('Escape', { shiftKey: true }), want: null },
    { name: '← is prev', event: key('ArrowLeft'), want: 'prev' },
    { name: 'k is prev', event: key('k'), want: 'prev' },
    { name: '→ is next', event: key('ArrowRight'), want: 'next' },
    { name: 'j is next', event: key('j'), want: 'next' },
    { name: 'e edits', event: key('e'), want: 'edit' },
    { name: 'w toggles the width', event: key('w'), want: 'toggleWidth' },
    {
      name: 'Alt+← (history back) is left alone',
      event: key('ArrowLeft', { altKey: true }),
      want: null
    },
    { name: 'Cmd+→ is left alone', event: key('ArrowRight', { metaKey: true }), want: null },
    {
      name: 'Shift+→ (text selection) is left alone',
      event: key('ArrowRight', { shiftKey: true }),
      want: null
    },
    { name: 'typing j in a textarea', event: key('j', { target: el('TEXTAREA') }), want: null },
    { name: 'other keys', event: key('x'), want: null }
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(focusActionForKey(c.event)).toBe(c.want);
    });
  }
});

describe('focusPosition', () => {
  const order = ['a', 'b', 'c'];

  it('finds neighbours', () => {
    expect(focusPosition(order, 'a')).toEqual({ index: 0, total: 3, prevId: null, nextId: 'b' });
    expect(focusPosition(order, 'b')).toEqual({ index: 1, total: 3, prevId: 'a', nextId: 'c' });
    expect(focusPosition(order, 'c')).toEqual({ index: 2, total: 3, prevId: 'b', nextId: null });
  });

  it('has no neighbours for a bean outside the list without a last position', () => {
    expect(focusPosition(order, 'x')).toEqual({ index: -1, total: 3, prevId: null, nextId: null });
    expect(focusPosition([], 'x', 2)).toEqual({ index: -1, total: 0, prevId: null, nextId: null });
  });

  it('continues from where a bean left the list', () => {
    // 'b' (index 1) left: next is the bean that took its place, prev the one before.
    expect(focusPosition(['a', 'c'], 'b', 1)).toEqual({
      index: -1,
      total: 2,
      prevId: 'a',
      nextId: 'c'
    });
    // It was last: no next.
    expect(focusPosition(['a', 'b'], 'c', 2)).toEqual({
      index: -1,
      total: 2,
      prevId: 'b',
      nextId: null
    });
    // Out-of-range indices are clamped.
    expect(focusPosition(['a'], 'z', 9).prevId).toBe('a');
  });
});

describe('backlogOrder', () => {
  // p ─┬─ c1 ── g
  //    └─ c2
  // q
  // d (draft)
  const beans = [
    bean('p'),
    bean('c1', { parentId: 'p' }),
    bean('g', { parentId: 'c1' }),
    bean('c2', { parentId: 'p', status: 'in-progress' }),
    bean('q'),
    bean('d', { status: 'draft' })
  ];
  const childrenOf = (id: string) => beans.filter((b) => b.parentId === id);
  const byId = new Map(beans.map((b) => [b.id, b]));
  const sections = [[byId.get('p')!, byId.get('q')!], [byId.get('d')!]];

  it('lists roots depth-first with their children, section by section', () => {
    expect(backlogOrder({ sections, childrenOf, visible: null, isCollapsed: () => false })).toEqual(
      ['p', 'c1', 'g', 'c2', 'q', 'd']
    );
  });

  it('skips folded subtrees', () => {
    expect(
      backlogOrder({ sections, childrenOf, visible: null, isCollapsed: (id) => id === 'c1' })
    ).toEqual(['p', 'c1', 'c2', 'q', 'd']);
  });

  it('only lists visible beans under a filter, ignoring folds', () => {
    const visible = new Set(['p', 'c1', 'g', 'd']);
    expect(
      backlogOrder({
        sections: [[byId.get('p')!], [byId.get('d')!]],
        childrenOf,
        visible,
        isCollapsed: () => true
      })
    ).toEqual(['p', 'c1', 'g', 'd']);
  });

  it('survives parent cycles', () => {
    const cyclic = [bean('x', { parentId: 'y' }), bean('y', { parentId: 'x' })];
    expect(
      backlogOrder({
        sections: [[cyclic[0]]],
        childrenOf: (id) => cyclic.filter((b) => b.parentId === id),
        visible: null,
        isCollapsed: () => false
      })
    ).toEqual(['x', 'y']);
  });
});

describe('relatedBeans', () => {
  it('resolves parent, children and symmetric blocking links', () => {
    const beans = [
      bean('a', { parentId: 'p', blockingIds: ['b'], blockedByIds: ['c'] }),
      bean('p'),
      bean('kid', { parentId: 'a' }),
      bean('b'),
      bean('c'),
      // Inverse links declared on the other bean
      bean('d', { blockedByIds: ['a'] }),
      bean('e', { blockingIds: ['a'] }),
      // Duplicate of a's own link
      bean('c2', { blockingIds: [] })
    ];
    const r = relatedBeans(beans[0], beans);
    expect(r.parent?.id).toBe('p');
    expect(r.children.map((b) => b.id)).toEqual(['kid']);
    expect(r.blocking.map((b) => b.id)).toEqual(['b', 'd']);
    expect(r.blockedBy.map((b) => b.id)).toEqual(['c', 'e']);
    expect(hasRelated(r)).toBe(true);
  });

  it('dedupes links declared on both sides and drops unknown IDs', () => {
    const beans = [
      bean('a', { blockingIds: ['b', 'missing'] }),
      bean('b', { blockedByIds: ['a'] })
    ];
    const r = relatedBeans(beans[0], beans);
    expect(r.blocking.map((b) => b.id)).toEqual(['b']);
    expect(r.blockedBy).toEqual([]);
  });

  it('uses the lookup for referenced beans (e.g. an archived parent)', () => {
    const archivedParent = bean('p', { status: 'completed' });
    const a = bean('a', { parentId: 'p' });
    const r = relatedBeans(a, [a], (id) => (id === 'p' ? archivedParent : undefined));
    expect(r.parent).toBe(archivedParent);
    expect(hasRelated(relatedBeans(bean('lonely'), [bean('lonely')]))).toBe(false);
  });
});
