/**
 * Split backend messages (e.g. worktrunk hook approval errors) into text and
 * inline commands, so the UI can render `backticked` commands as copyable
 * code.
 */

export type MessageSegment =
  | { kind: 'text'; text: string }
  | {
      kind: 'command';
      /** The command as written in the message. */
      command: string;
      /**
       * What a copy button should copy: the command, prefixed with
       * `cd <dir> &&` when the message says to run it "in <dir>".
       */
      copyText: string;
    };

/** Quote a string for POSIX shells when it contains special characters. */
export function shellQuote(s: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(s)) return s;
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

// " in /abs/path" right after a command; the path ends at whitespace, a comma
// or parenthesis, or a sentence-ending period.
const IN_DIR = /^ in (\/[^\s,()]*?)(?=[.,]?(?:\s|$)|[()])/;

/** Split a message into text and `backticked` command segments. */
export function parseMessageSegments(message: string): MessageSegment[] {
  const parts = message.split('`');
  // An odd number of backticks leaves the last one unmatched: keep it as text.
  if (parts.length % 2 === 0) {
    parts[parts.length - 2] += '`' + parts.pop();
  }
  // Directory each command runs in. "run `a` and `b` in /dir" applies /dir
  // to both, so walk backwards and carry it over " and " / ", " joins.
  const dirs: (string | undefined)[] = [];
  for (let i = parts.length - 2; i >= 1; i -= 2) {
    const after = parts[i + 1] ?? '';
    dirs[i] =
      IN_DIR.exec(after)?.[1] ?? (/^(?: and |, |, and )$/.test(after) ? dirs[i + 2] : undefined);
  }

  const segments: MessageSegment[] = [];
  parts.forEach((part, i) => {
    if (i % 2 === 0) {
      if (part) segments.push({ kind: 'text', text: part });
      return;
    }
    if (part.trim() === '') {
      segments.push({ kind: 'text', text: '`' + part + '`' });
      return;
    }
    const dir = dirs[i];
    segments.push({
      kind: 'command',
      command: part,
      copyText: dir ? `cd ${shellQuote(dir)} && ${part}` : part
    });
  });
  return segments;
}
