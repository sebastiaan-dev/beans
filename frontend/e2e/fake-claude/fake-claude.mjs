// Fake Claude Code CLI used by the e2e tests in place of the real `claude`.
//
// beans-serve finds it via PATH (see fixtures.ts), so e2e tests never start a
// real agent. Behaviour is controlled through files in $FAKE_CLAUDE_DIR:
//
// - script.json: array of turns; each turn is an array of stream-json events
//   to emit in response to one user message. Turns are consumed in order across
//   process restarts (the server kills and resumes the agent for blocking tools
//   like AskUserQuestion). When the script is exhausted, a minimal reply is sent.
// - received.jsonl: every user message received on stdin is appended here,
//   so tests can assert exactly what the agent was sent.
// - turn: index of the next turn to play (managed by this script).
// - pids: the PID of every streaming-mode process, one per line, so tests can
//   check that the server stopped the agent. An empty turn ([]) emits nothing,
//   which keeps the agent busy until it is stopped.
import { appendFileSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createInterface } from 'node:readline';

const dir = process.env.FAKE_CLAUDE_DIR;
const streaming = process.argv.includes('stream-json');

if (!streaming || !dir) {
  // One-shot `claude --print` calls (titles, quick replies): consume the prompt
  // and answer with nothing.
  process.stdin.resume();
  process.stdin.on('end', () => process.exit(0));
} else {
  appendFileSync(join(dir, 'pids'), `${process.pid}\n`);
  const turnFile = join(dir, 'turn');
  const scriptFile = join(dir, 'script.json');
  const emit = (event) => process.stdout.write(JSON.stringify(event) + '\n');

  const nextTurn = () => {
    const script = existsSync(scriptFile) ? JSON.parse(readFileSync(scriptFile, 'utf-8')) : [];
    const index = existsSync(turnFile) ? Number(readFileSync(turnFile, 'utf-8')) : 0;
    writeFileSync(turnFile, String(index + 1));
    return script[index] ?? null;
  };

  const lines = createInterface({ input: process.stdin });
  lines.on('line', (line) => {
    if (!line.trim()) return;
    const msg = JSON.parse(line);
    appendFileSync(
      join(dir, 'received.jsonl'),
      JSON.stringify({ args: process.argv.slice(2), message: msg.message }) + '\n'
    );

    const turn = nextTurn() ?? [
      {
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'OK.' }] }
      },
      { type: 'result', subtype: 'success', session_id: 'fake-session' }
    ];
    for (const event of turn) emit(event);
  });
  lines.on('close', () => process.exit(0));
}
