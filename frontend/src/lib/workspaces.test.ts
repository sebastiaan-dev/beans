import { describe, expect, it } from 'vitest';
import {
  isDetached,
  slugifyWorkspaceName,
  validateWorkspaceName,
  workspaceLabel,
  workspaceTooltip
} from './workspaces';

describe('validateWorkspaceName', () => {
  // Mirrors TestValidateName in internal/worktree/name_test.go.
  const valid = [
    'brave-otter-x7k2',
    'run-test',
    'Feature_1.2',
    'a',
    'v1.2',
    '9lives',
    'a'.repeat(100)
  ];
  const alsoInvalid = [
    '../escape',
    '_x',
    'feature..x',
    'name~1',
    'a^b',
    'a:b',
    'a?b',
    'a*b',
    'a[b',
    'a\\b',
    'a@{b',
    'ünïcode',
    'tab\tname'
  ];
  for (const name of alsoInvalid) {
    it(`rejects ${JSON.stringify(name)}`, () => {
      expect(validateWorkspaceName(name)).not.toBeNull();
    });
  }
  for (const name of valid) {
    it(`accepts ${JSON.stringify(name)}`, () => {
      expect(validateWorkspaceName(name)).toBeNull();
    });
  }

  const invalid: { name: string; reason: RegExp; suggestion: string | null }[] = [
    { name: '', reason: /must not be empty/, suggestion: null },
    { name: 'a'.repeat(101), reason: /at most 100 characters/, suggestion: 'a'.repeat(100) },
    { name: 'fix/foo', reason: /only letters, digits/, suggestion: 'fix-foo' },
    { name: 'my feature', reason: /only letters, digits/, suggestion: 'my-feature' },
    { name: '-lead', reason: /start with a letter or digit/, suggestion: 'lead' },
    { name: '.hidden', reason: /start with a letter or digit/, suggestion: 'hidden' },
    { name: 'a..b', reason: /must not contain '\.\.'/, suggestion: 'a-b' },
    { name: 'trailing.', reason: /must not end with/, suggestion: 'trailing' },
    { name: 'x.lock', reason: /must not end with/, suggestion: 'x-lock' },
    { name: 'naïve', reason: /only letters, digits/, suggestion: 'na-ve' },
    { name: '///', reason: /only letters, digits/, suggestion: null },
    { name: 'ab~', reason: /only letters, digits/, suggestion: 'ab' }
  ];
  for (const c of invalid) {
    it(`rejects ${JSON.stringify(c.name)}`, () => {
      const res = validateWorkspaceName(c.name);
      expect(res).not.toBeNull();
      expect(res!.reason).toMatch(c.reason);
      expect(res!.suggestion).toBe(c.suggestion);
      if (res!.suggestion) expect(validateWorkspaceName(res!.suggestion)).toBeNull();
    });
  }

  it('counts bytes like the backend', () => {
    // 50 two-byte characters = 100 bytes, 51 = 102 bytes.
    expect(validateWorkspaceName('é'.repeat(51))!.reason).toMatch(/at most 100/);
  });
});

describe('slugifyWorkspaceName', () => {
  it('slugifies', () => {
    expect(slugifyWorkspaceName('Run Test')).toBe('run-test');
    expect(slugifyWorkspaceName('  --Fix/Login Bug!! ')).toBe('fix-login-bug');
    expect(slugifyWorkspaceName('???')).toBe('');
  });

  it('trims to the maximum length without a trailing dash', () => {
    const s = slugifyWorkspaceName('a'.repeat(99) + ' b');
    expect(s).toBe('a'.repeat(99));
  });
});

describe('workspace display', () => {
  const wt = { id: 'fix-foo', name: 'fix/foo', branch: 'fix/foo', path: '/wt/repo.fix-foo' };

  it('labels by name, branch, directory, then ID', () => {
    expect(workspaceLabel(wt)).toBe('fix/foo');
    expect(workspaceLabel({ ...wt, name: null })).toBe('fix/foo');
    expect(workspaceLabel({ ...wt, name: '', branch: '' })).toBe('repo.fix-foo');
    expect(workspaceLabel({ ...wt, name: null, branch: '', path: '' })).toBe('fix-foo');
  });

  it('detects detached worktrees', () => {
    expect(isDetached(wt)).toBe(false);
    expect(isDetached({ ...wt, branch: '' })).toBe(true);
  });

  it('builds a tooltip with branch and path', () => {
    expect(workspaceTooltip(wt, [null, 'Agent running'])).toBe(
      'fix/foo\nBranch: fix/foo\nPath: /wt/repo.fix-foo\nAgent running'
    );
    expect(workspaceTooltip({ ...wt, name: 'x', branch: '' })).toBe(
      'x\nDetached HEAD (no branch)\nPath: /wt/repo.fix-foo'
    );
  });
});
