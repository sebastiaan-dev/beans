import { describe, expect, it } from 'vitest';
import {
  FoldModel,
  foldStorageKey,
  parentIds,
  parseCollapsed,
  serializeCollapsed,
  type KeyValueStorage
} from './fold';

class MemoryStorage implements KeyValueStorage {
  data = new Map<string, string>();
  getItem(key: string) {
    return this.data.get(key) ?? null;
  }
  setItem(key: string, value: string) {
    this.data.set(key, value);
  }
  removeItem(key: string) {
    this.data.delete(key);
  }
}

describe('parseCollapsed', () => {
  const cases: { name: string; raw: string | null; want: string[] }[] = [
    { name: 'null', raw: null, want: [] },
    { name: 'empty string', raw: '', want: [] },
    { name: 'invalid JSON', raw: '{nope', want: [] },
    { name: 'non-array', raw: '{"a":1}', want: [] },
    { name: 'ids', raw: '["a","b"]', want: ['a', 'b'] },
    { name: 'drops non-strings and empties', raw: '["a",1,null,"",{"x":1},"b"]', want: ['a', 'b'] }
  ];
  for (const tc of cases) {
    it(tc.name, () => {
      expect([...parseCollapsed(tc.raw)]).toEqual(tc.want);
    });
  }

  it('round-trips through serializeCollapsed', () => {
    expect([...parseCollapsed(serializeCollapsed(['b', 'a']))]).toEqual(['a', 'b']);
  });
});

describe('foldStorageKey', () => {
  it('includes the project name', () => {
    expect(foldStorageKey('beans')).toBe('beans-folded:beans');
    expect(foldStorageKey('other')).not.toBe(foldStorageKey('beans'));
  });
});

describe('parentIds', () => {
  it('returns beans with at least one present child', () => {
    const beans = [
      { id: 'm', parentId: null },
      { id: 'e', parentId: 'm' },
      { id: 't1', parentId: 'e' },
      { id: 't2', parentId: 'e' },
      { id: 'orphan', parentId: 'missing' }
    ];
    expect(parentIds(beans).sort()).toEqual(['e', 'm']);
  });

  it('is empty without children', () => {
    expect(parentIds([{ id: 'a' }, { id: 'b', parentId: null }])).toEqual([]);
  });
});

describe('FoldModel', () => {
  it('starts expanded and toggles', () => {
    const m = new FoldModel(null);
    expect(m.isCollapsed('a')).toBe(false);
    expect(m.toggle('a')).toBe(true);
    expect(m.isCollapsed('a')).toBe(true);
    expect(m.toggle('a')).toBe(true);
    expect(m.isCollapsed('a')).toBe(false);
  });

  it('reports no-op changes', () => {
    const m = new FoldModel(null);
    expect(m.setCollapsed('a', false)).toBe(false);
    expect(m.setCollapsed('a', true)).toBe(true);
    expect(m.setCollapsed('a', true)).toBe(false);
    expect(m.collapseAll(['a'])).toBe(false);
    expect(m.expandAll()).toBe(true);
    expect(m.expandAll()).toBe(false);
  });

  it('persists per project', () => {
    const storage = new MemoryStorage();
    const m = new FoldModel(storage);
    m.setProject('alpha');
    m.setCollapsed('a1', true);
    m.setProject('beta');
    expect(m.isCollapsed('a1')).toBe(false);
    m.setCollapsed('b1', true);

    expect(storage.getItem(foldStorageKey('alpha'))).toBe('["a1"]');
    expect(storage.getItem(foldStorageKey('beta'))).toBe('["b1"]');

    // A fresh model (e.g. after reload) restores the state.
    const reloaded = new FoldModel(storage);
    reloaded.setProject('alpha');
    expect(reloaded.isCollapsed('a1')).toBe(true);
    expect(reloaded.isCollapsed('b1')).toBe(false);
  });

  it('setProject is a no-op for the active project', () => {
    const m = new FoldModel(new MemoryStorage());
    expect(m.setProject('alpha')).toBe(true);
    m.setCollapsed('x', true);
    expect(m.setProject('alpha')).toBe(false);
    expect(m.isCollapsed('x')).toBe(true);
  });

  it('collapseAll replaces stale ids and expandAll clears storage', () => {
    const storage = new MemoryStorage();
    const m = new FoldModel(storage);
    m.setProject('p');
    m.setCollapsed('gone', true);
    m.collapseAll(['a', 'b']);
    expect([...m.collapsed].sort()).toEqual(['a', 'b']);
    expect(storage.getItem(foldStorageKey('p'))).toBe('["a","b"]');
    m.expandAll();
    expect(m.collapsed.size).toBe(0);
    expect(storage.getItem(foldStorageKey('p'))).toBeNull();
  });

  it('survives throwing storage', () => {
    const broken: KeyValueStorage = {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => {
        throw new Error('full');
      },
      removeItem: () => {
        throw new Error('denied');
      }
    };
    const m = new FoldModel(broken);
    m.setProject('p');
    expect(m.toggle('a')).toBe(true);
    expect(m.isCollapsed('a')).toBe(true);
  });
});
