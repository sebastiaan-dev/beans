import { describe, expect, it } from 'vitest';
import type { KeyValueStorage } from './fold';
import {
  SIDEBAR_COLLAPSED_KEY,
  initialSidebarCollapsed,
  isSidebarToggleShortcut,
  persistSidebarCollapsed,
  workspaceInitials
} from './sidebar';

function storageWith(value: string | null): KeyValueStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  if (value !== null) data.set(SIDEBAR_COLLAPSED_KEY, value);
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k)
  };
}

describe('initialSidebarCollapsed', () => {
  const cases: { name: string; saved: string | null; width: number; want: boolean }[] = [
    { name: 'saved true wins on wide screens', saved: 'true', width: 1600, want: true },
    { name: 'saved false wins on narrow screens', saved: 'false', width: 500, want: false },
    { name: 'no preference, wide', saved: null, width: 1280, want: false },
    { name: 'no preference, narrow', saved: null, width: 600, want: true },
    { name: 'garbage treated as no preference', saved: 'maybe', width: 1280, want: false },
    { name: 'unknown width', saved: null, width: 0, want: false }
  ];
  for (const tc of cases) {
    it(tc.name, () => {
      expect(initialSidebarCollapsed(storageWith(tc.saved), tc.width)).toBe(tc.want);
    });
  }

  it('handles missing storage', () => {
    expect(initialSidebarCollapsed(null, 1280)).toBe(false);
  });
});

describe('persistSidebarCollapsed', () => {
  it('round-trips', () => {
    const s = storageWith(null);
    persistSidebarCollapsed(s, true);
    expect(initialSidebarCollapsed(s, 1280)).toBe(true);
    persistSidebarCollapsed(s, false);
    expect(initialSidebarCollapsed(s, 500)).toBe(false);
  });
});

describe('isSidebarToggleShortcut', () => {
  const base = { key: '\\', metaKey: false, ctrlKey: false, altKey: false, shiftKey: false };
  const cases: { name: string; e: Parameters<typeof isSidebarToggleShortcut>[0]; want: boolean }[] =
    [
      { name: 'cmd+backslash', e: { ...base, metaKey: true }, want: true },
      { name: 'ctrl+backslash', e: { ...base, ctrlKey: true }, want: true },
      { name: 'plain backslash', e: base, want: false },
      { name: 'with shift', e: { ...base, metaKey: true, shiftKey: true }, want: false },
      { name: 'with alt', e: { ...base, ctrlKey: true, altKey: true }, want: false },
      { name: 'other key', e: { ...base, metaKey: true, key: 'b' }, want: false },
      {
        name: 'already handled',
        e: { ...base, metaKey: true, defaultPrevented: true },
        want: false
      },
      {
        name: 'inside terminal',
        e: {
          ...base,
          ctrlKey: true,
          target: { closest: (s: string) => (s === '.xterm' ? {} : null) } as unknown as EventTarget
        },
        want: false
      },
      {
        name: 'outside terminal',
        e: { ...base, ctrlKey: true, target: { closest: () => null } as unknown as EventTarget },
        want: true
      }
    ];
  for (const tc of cases) {
    it(tc.name, () => {
      expect(isSidebarToggleShortcut(tc.e)).toBe(tc.want);
    });
  }
});

describe('workspaceInitials', () => {
  const cases: [string, string][] = [
    ['main', 'MA'],
    ['brave-otter', 'BO'],
    ['feature/login page', 'FL'],
    ['x', 'X'],
    ['', '?'],
    ['--', '?'],
    ['élan vital', 'ÉV'],
    ['fix_bug_123', 'FB']
  ];
  for (const [label, want] of cases) {
    it(`${JSON.stringify(label)} -> ${want}`, () => {
      expect(workspaceInitials(label)).toBe(want);
    });
  }
});
