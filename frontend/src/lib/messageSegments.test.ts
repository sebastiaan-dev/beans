import { describe, expect, it } from 'vitest';
import { parseMessageSegments, shellQuote } from './messageSegments';

describe('parseMessageSegments', () => {
  it('returns plain text unchanged', () => {
    expect(parseMessageSegments('setup failed')).toEqual([{ kind: 'text', text: 'setup failed' }]);
  });

  it('extracts commands and their directories', () => {
    const msg =
      'Review and approve them by running `wt config approvals add` in /tmp/repo, then run ' +
      "`wt hook pre-start` and `wt hook post-start` in /tmp/wt/feat (beans' worktree.setup command, if any, was not run either).";
    const commands = parseMessageSegments(msg).filter((s) => s.kind === 'command');
    expect(commands).toEqual([
      {
        kind: 'command',
        command: 'wt config approvals add',
        copyText: 'cd /tmp/repo && wt config approvals add'
      },
      {
        kind: 'command',
        command: 'wt hook pre-start',
        copyText: 'cd /tmp/wt/feat && wt hook pre-start'
      },
      {
        kind: 'command',
        command: 'wt hook post-start',
        copyText: 'cd /tmp/wt/feat && wt hook post-start'
      }
    ]);
    // Text and commands concatenate back to the message without backticks.
    const joined = parseMessageSegments(msg)
      .map((s) => (s.kind === 'text' ? s.text : s.command))
      .join('');
    expect(joined).toBe(msg.replaceAll('`', ''));
  });

  it('handles a directory at the end of a sentence', () => {
    const [, cmd] = parseMessageSegments('run `wt config approvals add` in /a/b, then try again.');
    expect(cmd).toMatchObject({ copyText: 'cd /a/b && wt config approvals add' });
    const [, cmd2] = parseMessageSegments('run `x` in /a/b.');
    expect(cmd2).toMatchObject({ copyText: 'cd /a/b && x' });
  });

  it('ignores relative "in" phrases', () => {
    const [, cmd] = parseMessageSegments('run `x` in the repo');
    expect(cmd).toMatchObject({ copyText: 'x' });
  });

  it('keeps unmatched and empty backticks as text', () => {
    expect(parseMessageSegments('a `b` c `d')).toEqual([
      { kind: 'text', text: 'a ' },
      { kind: 'command', command: 'b', copyText: 'b' },
      { kind: 'text', text: ' c `d' }
    ]);
    expect(parseMessageSegments('a `` b')).toEqual([
      { kind: 'text', text: 'a ' },
      { kind: 'text', text: '``' },
      { kind: 'text', text: ' b' }
    ]);
  });
});

describe('shellQuote', () => {
  it('quotes only when needed', () => {
    expect(shellQuote('/tmp/a-b_c.d')).toBe('/tmp/a-b_c.d');
    expect(shellQuote('/tmp/my repo')).toBe(`'/tmp/my repo'`);
    expect(shellQuote(`it's`)).toBe(`'it'\\''s'`);
  });
});
