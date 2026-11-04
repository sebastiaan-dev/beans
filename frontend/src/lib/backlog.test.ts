import { describe, expect, it } from 'vitest';
import type { Bean } from '$lib/beans.svelte';
import { backlogRoots, isBacklogRoot } from './backlog';

function bean(id: string, status: string, parentId: string | null = null): Bean {
  return { id, status, parentId } as Bean;
}

describe('isBacklogRoot', () => {
  const cases: { name: string; beans: Bean[]; id: string; want: boolean }[] = [
    { name: 'top-level todo', beans: [bean('a', 'todo')], id: 'a', want: true },
    { name: 'top-level draft', beans: [bean('a', 'draft')], id: 'a', want: true },
    { name: 'non-backlog status', beans: [bean('a', 'in-progress')], id: 'a', want: false },
    {
      name: 'child of todo parent is nested',
      beans: [bean('p', 'todo'), bean('c', 'todo', 'p')],
      id: 'c',
      want: false
    },
    {
      name: 'child of completed parent is a root',
      beans: [bean('p', 'completed'), bean('c', 'todo', 'p')],
      id: 'c',
      want: true
    },
    {
      name: 'child of in-progress parent is a root',
      beans: [bean('p', 'in-progress'), bean('c', 'draft', 'p')],
      id: 'c',
      want: true
    },
    {
      name: 'grandchild under todo grandparent is nested',
      beans: [bean('g', 'todo'), bean('p', 'completed', 'g'), bean('c', 'todo', 'p')],
      id: 'c',
      want: false
    },
    {
      name: 'unknown or archived parent ends the chain',
      beans: [bean('c', 'todo', 'missing')],
      id: 'c',
      want: true
    },
    {
      name: 'parent cycle terminates',
      beans: [bean('a', 'completed', 'b'), bean('b', 'completed', 'a'), bean('c', 'todo', 'a')],
      id: 'c',
      want: true
    }
  ];

  for (const tc of cases) {
    it(tc.name, () => {
      const byId = new Map(tc.beans.map((b) => [b.id, b]));
      expect(isBacklogRoot(byId.get(tc.id)!, byId)).toBe(tc.want);
    });
  }
});

describe('backlogRoots', () => {
  it('returns roots of a section in input order', () => {
    const beans = [
      bean('t1', 'todo'),
      bean('p', 'in-progress'),
      bean('c1', 'todo', 'p'),
      bean('c2', 'todo', 't1'),
      bean('d1', 'draft', 'p')
    ];
    expect(backlogRoots(beans, 'todo').map((b) => b.id)).toEqual(['t1', 'c1']);
    expect(backlogRoots(beans, 'draft').map((b) => b.id)).toEqual(['d1']);
  });
});
