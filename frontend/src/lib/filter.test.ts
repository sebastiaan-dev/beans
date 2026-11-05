import { describe, expect, it } from 'vitest';
import type { Bean } from '$lib/beans.svelte';
import {
  blockedSelection,
  clearClauses,
  compileFilter,
  createFilterContext,
  filterTree,
  formatFilter,
  invalidValues,
  isEmptyFilter,
  matchesText,
  parseFilter,
  removeTerm,
  selectedValues,
  setBlocked,
  setClause,
  suggest,
  toggleValue,
  tokenize,
  type FilterClause
} from './filter';

function bean(id: string, fields: Partial<Bean> = {}): Bean {
  return {
    id,
    slug: '',
    path: `${id}.md`,
    title: id,
    status: 'todo',
    type: 'task',
    priority: '',
    tags: [],
    createdAt: '',
    updatedAt: '',
    body: '',
    order: '',
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    worktreeId: null,
    ...fields
  } as Bean;
}

function run(beans: Bean[], query: string): string[] {
  const match = compileFilter(query, createFilterContext(beans));
  return beans.filter(match).map((b) => b.id);
}

describe('tokenize', () => {
  it('splits on whitespace and keeps quoted sections', () => {
    expect(tokenize(' a  parent:"My Epic",b "two words"').map((t) => t.raw)).toEqual([
      'a',
      'parent:"My Epic",b',
      '"two words"'
    ]);
  });

  it('records token positions', () => {
    expect(tokenize('ab cd')).toEqual([
      { raw: 'ab', start: 0, end: 2 },
      { raw: 'cd', start: 3, end: 5 }
    ]);
  });
});

describe('parseFilter', () => {
  const cases: { query: string; terms: unknown[] }[] = [
    { query: '', terms: [] },
    { query: 'auth', terms: [{ kind: 'text', value: 'auth' }] },
    {
      query: 'status:todo,Draft',
      terms: [
        { kind: 'clause', field: 'status', negate: false, op: '=', values: ['todo', 'draft'] }
      ]
    },
    {
      query: 'priority:>=high',
      terms: [{ kind: 'clause', field: 'priority', negate: false, op: '>=', values: ['high'] }]
    },
    {
      query: '-type:bug',
      terms: [{ kind: 'clause', field: 'type', negate: true, op: '=', values: ['bug'] }]
    },
    {
      query: 'tags:ui prio:low under:abc',
      terms: [
        { kind: 'clause', field: 'tag', negate: false, op: '=', values: ['ui'] },
        { kind: 'clause', field: 'priority', negate: false, op: '=', values: ['low'] },
        { kind: 'clause', field: 'parent', negate: false, op: '=', values: ['abc'] }
      ]
    },
    {
      query: 'milestone:"Version 0.2"',
      terms: [
        { kind: 'clause', field: 'milestone', negate: false, op: '=', values: ['Version 0.2'] }
      ]
    },
    {
      query: 'status:',
      terms: [{ kind: 'clause', field: 'status', negate: false, op: '=', values: [] }]
    },
    { query: 'http://x', terms: [{ kind: 'text', value: 'http://x' }] },
    { query: '"two words"', terms: [{ kind: 'text', value: 'two words' }] }
  ];
  for (const { query, terms } of cases) {
    it(`parses ${JSON.stringify(query)}`, () => {
      expect(parseFilter(query).terms).toEqual(terms);
    });
  }

  it('treats incomplete clauses as empty filters', () => {
    expect(isEmptyFilter(parseFilter('status: '))).toBe(true);
    expect(isEmptyFilter(parseFilter('status:todo'))).toBe(false);
  });
});

describe('formatFilter', () => {
  it('round-trips canonical queries', () => {
    for (const q of [
      'status:todo,draft priority:>=high auth',
      '-tag:ui milestone:"Version 0.2" "two words"',
      'blocked:no'
    ]) {
      expect(formatFilter(parseFilter(q))).toBe(q);
    }
  });
});

describe('invalidValues', () => {
  it('flags unknown status/type/priority/blocked values', () => {
    const clause = (q: string) => parseFilter(q).terms[0] as FilterClause;
    expect(invalidValues(clause('status:todo,nope'))).toEqual(['nope']);
    expect(invalidValues(clause('priority:>=urgent'))).toEqual(['urgent']);
    expect(invalidValues(clause('blocked:maybe'))).toEqual(['maybe']);
    expect(invalidValues(clause('tag:anything'))).toEqual([]);
  });
});

describe('editing', () => {
  it('toggles values in the plain clause', () => {
    let q = parseFilter('auth');
    q = toggleValue(q, 'status', 'todo');
    expect(formatFilter(q)).toBe('auth status:todo');
    q = toggleValue(q, 'status', 'draft');
    expect(formatFilter(q)).toBe('auth status:todo,draft');
    expect(selectedValues(q, 'status')).toEqual(['todo', 'draft']);
    q = toggleValue(toggleValue(q, 'status', 'todo'), 'status', 'draft');
    expect(formatFilter(q)).toBe('auth');
  });

  it('leaves negated and comparison clauses alone', () => {
    const q = toggleValue(parseFilter('priority:>=high -priority:critical'), 'priority', 'low');
    expect(formatFilter(q)).toBe('priority:>=high -priority:critical priority:low');
  });

  it('replaces clauses with setClause in place', () => {
    const q = setClause(parseFilter('a parent:x b parent:y'), 'parent', ['z']);
    expect(formatFilter(q)).toBe('a parent:z b');
    expect(formatFilter(setClause(q, 'parent', []))).toBe('a b');
  });

  it('sets and reads blocked', () => {
    let q = setBlocked(parseFilter('x'), true);
    expect(formatFilter(q)).toBe('x blocked:yes');
    expect(blockedSelection(q)).toBe(true);
    q = setBlocked(q, false);
    expect(blockedSelection(q)).toBe(false);
    expect(blockedSelection(setBlocked(q, null))).toBe(null);
  });

  it('removes terms and clears clauses', () => {
    const q = parseFilter('status:todo auth tag:ui');
    expect(formatFilter(removeTerm(q, 0))).toBe('auth tag:ui');
    expect(formatFilter(clearClauses(q))).toBe('auth');
  });
});

describe('compileFilter', () => {
  const beans = [
    bean('beans-m1', { type: 'milestone', title: 'Version 0.2', slug: 'version-0-2' }),
    bean('beans-e1', { type: 'epic', title: 'Login Epic', parentId: 'beans-m1' }),
    bean('beans-a', {
      title: 'Auth Feature',
      type: 'feature',
      priority: 'high',
      tags: ['UI'],
      parentId: 'beans-e1'
    }),
    bean('beans-b', { title: 'DB Bug', type: 'bug', status: 'draft', priority: 'critical' }),
    bean('beans-c', { title: 'Cleanup', status: 'in-progress', priority: 'low', tags: ['chore'] }),
    bean('beans-d', { title: 'Default prio', status: 'completed' }),
    bean('beans-x', { title: 'Blocker', blockingIds: ['beans-c'] }),
    bean('beans-y', { title: 'Done blocker', status: 'completed', blockingIds: ['beans-d'] }),
    bean('beans-z', { title: 'Blocked via field', blockedByIds: ['beans-b'] }),
    bean('beans-zc', { title: 'Child of blocked', parentId: 'beans-z' })
  ];

  const cases: { query: string; want: string[] }[] = [
    { query: '', want: beans.map((b) => b.id) },
    { query: 'auth', want: ['beans-a'] },
    { query: 'AUTH feature', want: ['beans-a'] },
    { query: 'bug', want: ['beans-b'] },
    { query: 'status:draft,in-progress', want: ['beans-b', 'beans-c'] },
    { query: '-status:todo', want: ['beans-b', 'beans-c', 'beans-d', 'beans-y'] },
    { query: 'type:bug,feature', want: ['beans-a', 'beans-b'] },
    { query: 'priority:>=high', want: ['beans-a', 'beans-b'] },
    { query: 'priority:>high', want: ['beans-b'] },
    { query: 'priority:<=low', want: ['beans-c'] },
    { query: 'priority:<normal', want: ['beans-c'] },
    { query: 'priority:normal status:completed', want: ['beans-d', 'beans-y'] },
    { query: 'tag:ui', want: ['beans-a'] },
    { query: 'tag:ui,chore', want: ['beans-a', 'beans-c'] },
    { query: 'milestone:version-0-2', want: ['beans-e1', 'beans-a'] },
    { query: 'milestone:"version 0.2"', want: ['beans-e1', 'beans-a'] },
    { query: 'milestone:m1', want: ['beans-e1', 'beans-a'] },
    { query: 'epic:beans-m1', want: [] },
    { query: 'epic:"Login Epic"', want: ['beans-a'] },
    { query: 'parent:beans-e1', want: ['beans-a'] },
    { query: 'parent:nope', want: [] },
    { query: 'blocked:yes', want: ['beans-c', 'beans-z', 'beans-zc'] },
    { query: 'blocked:no type:bug', want: ['beans-b'] },
    { query: 'status:nope', want: [] },
    { query: 'status: auth', want: ['beans-a'] }
  ];
  for (const { query, want } of cases) {
    it(`matches ${JSON.stringify(query)}`, () => {
      expect(run(beans, query)).toEqual(want);
    });
  }

  it('survives parent cycles', () => {
    const cyclic = [bean('a', { parentId: 'b' }), bean('b', { parentId: 'a' })];
    expect(run(cyclic, 'parent:a')).toEqual(['b']);
    expect(run(cyclic, 'blocked:no')).toEqual(['a', 'b']);
  });
});

describe('matchesText', () => {
  it('matches title, type, status, id and tags', () => {
    const b = bean('beans-q1', { title: 'Hello', type: 'epic', status: 'draft', tags: ['ux'] });
    for (const t of ['hell', 'EPIC', 'draf', 'q1', 'ux', '']) expect(matchesText(b, t)).toBe(true);
    expect(matchesText(b, 'nope')).toBe(false);
  });
});

describe('filterTree', () => {
  it('adds ancestors of matches as visible context', () => {
    const beans = [
      bean('root'),
      bean('mid', { parentId: 'root' }),
      bean('leaf', { parentId: 'mid', type: 'bug' }),
      bean('other')
    ];
    const ctx = createFilterContext(beans);
    const { matched, visible } = filterTree(beans, compileFilter('type:bug', ctx), ctx.byId);
    expect([...matched]).toEqual(['leaf']);
    expect([...visible].sort()).toEqual(['leaf', 'mid', 'root']);
  });
});

describe('suggest', () => {
  const sources = {
    tags: ['ui', 'backend', 'urgent'],
    beans: [
      { id: 'beans-m1', title: 'Version 0.2', type: 'milestone' },
      { id: 'beans-e1', title: 'Login Epic', type: 'epic' }
    ]
  };
  const labels = (q: string, caret = q.length) => suggest(q, caret, sources).map((s) => s.label);
  const apply = (q: string) => {
    const [s] = suggest(q, q.length, sources);
    return q.slice(0, s.start) + s.insert + q.slice(s.end);
  };

  it('suggests field names', () => {
    expect(labels('st')).toEqual(['status:']);
    expect(labels('-ty')).toEqual(['-type:']);
    expect(labels('auth p')).toEqual(['priority:', 'parent:']);
    expect(apply('foo sta')).toBe('foo status:');
  });

  it('suggests values for the last comma part', () => {
    expect(labels('status:')).toEqual(['draft', 'todo', 'in-progress', 'completed', 'scrapped']);
    expect(labels('status:todo,d')).toEqual(['draft']);
    expect(apply('status:todo,d')).toBe('status:todo,draft ');
    expect(labels('priority:>=h')).toEqual(['high']);
    expect(apply('priority:>=h')).toBe('priority:>=high ');
    expect(labels('tag:u')).toEqual(['ui', 'urgent']);
    expect(labels('blocked:')).toEqual(['yes', 'no']);
  });

  it('suggests beans for parent references', () => {
    expect(labels('milestone:')).toEqual(['Version 0.2']);
    expect(labels('parent:log')).toEqual(['Login Epic']);
    expect(apply('epic:lo')).toBe('epic:beans-e1 ');
  });

  it('returns nothing for plain text or unknown fields', () => {
    expect(labels('')).toEqual([]);
    expect(labels('auth ')).toEqual([]);
    expect(labels('foo:')).toEqual([]);
  });

  it('only considers the text before the caret', () => {
    expect(labels('status:todo auth', 4)).toEqual(['status:']);
  });
});
