import { test as base } from '@playwright/test';
import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { connect } from 'node:net';
import { tmpdir } from 'node:os';
import { delimiter, join } from 'node:path';
import { BacklogPage } from './pages/backlog-page';
import { BoardPage } from './pages/board-page';

const PROJECT_ROOT = join(import.meta.dirname, '../..');
const BASE_PORT = 22900;

/**
 * Pick a unique port for a test's beans-serve instance.
 *
 * BEANS_E2E_BASE_PORT confines all servers of a run to [base, base + 100),
 * which lets several e2e runs share a machine without port collisions.
 */
function serverPort(workerIndex: number, parallelIndex: number): number {
  const override = process.env.BEANS_E2E_BASE_PORT;
  if (override) {
    return Number(override) + ((workerIndex * 10 + parallelIndex) % 100);
  }
  return BASE_PORT + workerIndex * 100 + parallelIndex;
}

const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: 'test',
  GIT_AUTHOR_EMAIL: 'test@test',
  GIT_COMMITTER_NAME: 'test',
  GIT_COMMITTER_EMAIL: 'test@test'
};

function getBinaries() {
  const beans = process.env.BEANS_BINARY;
  const beansServe = process.env.BEANS_SERVE_BINARY;
  if (!beans || !beansServe) {
    throw new Error('BEANS_BINARY and BEANS_SERVE_BINARY must be set — run tests via e2e/run.sh');
  }
  return { beans, beansServe };
}

/**
 * Create a beans template directory via `beans init`.
 * Called once per worker. Each test gets a fresh git repo but copies the
 * pre-initialized .beans directory to avoid the expensive CLI invocation.
 */
function createBeansTemplate(beansBin: string): string {
  const templateDir = mkdtempSync(join(tmpdir(), 'beans-e2e-template-'));
  execFileSync('git', ['init', '-b', 'main'], { cwd: templateDir, timeout: 10_000 });
  execFileSync('git', ['commit', '--allow-empty', '-m', 'init'], {
    cwd: templateDir,
    timeout: 10_000,
    env: GIT_ENV
  });
  execFileSync(beansBin, ['init'], {
    cwd: templateDir,
    encoding: 'utf-8',
    timeout: 10_000
  });
  return templateDir;
}

/**
 * Wait for a server to start accepting TCP connections, then verify HTTP.
 * Uses TCP connect first (faster than full HTTP fetch for early polls).
 */
async function waitForServer(port: number, timeoutMs = 10_000): Promise<void> {
  const start = Date.now();
  // First, wait for TCP port to open (much faster than HTTP fetch)
  while (Date.now() - start < timeoutMs) {
    const connected = await new Promise<boolean>((resolve) => {
      const socket = connect(port, '127.0.0.1');
      socket.once('connect', () => {
        socket.destroy();
        resolve(true);
      });
      socket.once('error', () => {
        socket.destroy();
        resolve(false);
      });
    });
    if (connected) break;
    await new Promise((r) => setTimeout(r, 25));
  }
  // Then verify HTTP is actually ready
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await fetch(`http://localhost:${port}/`);
      if (res.ok) return;
    } catch {
      // not ready yet
    }
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error(`Server on port ${port} did not start within ${timeoutMs}ms`);
}

/** Directory containing the fake `claude` CLI (see fake-claude/fake-claude.mjs). */
const FAKE_CLAUDE_BIN = join(import.meta.dirname, 'fake-claude');

/**
 * Environment for beans-serve. The fake `claude` shadows any real Claude Code
 * install on PATH, so agent sessions in e2e tests never start a real agent.
 */
function serverEnv(fakeClaudeDir: string): NodeJS.ProcessEnv {
  return {
    ...process.env,
    GIN_MODE: 'release',
    PATH: `${FAKE_CLAUDE_BIN}${delimiter}${process.env.PATH ?? ''}`,
    FAKE_CLAUDE_DIR: fakeClaudeDir
  };
}

/**
 * Helper to run beans CLI commands against a specific beans path.
 */
class BeansCLI {
  constructor(
    readonly beansPath: string,
    readonly projectDir: string,
    private binaryPath: string,
    readonly baseURL: string,
    /** State directory of the fake claude CLI ($FAKE_CLAUDE_DIR). */
    readonly fakeClaudeDir: string
  ) {}

  run(args: string[]): string {
    return execFileSync(this.binaryPath, ['--beans-path', this.beansPath, ...args], {
      cwd: this.projectDir,
      encoding: 'utf-8',
      timeout: 10_000
    });
  }

  create(title: string, opts: { type?: string; status?: string; priority?: string } = {}): string {
    const args = ['create', '--json', title, '-t', opts.type ?? 'task'];
    if (opts.status) args.push('-s', opts.status);
    if (opts.priority) args.push('-p', opts.priority);
    const output = this.run(args);
    const json = JSON.parse(output);
    return (json.bean?.id ?? json.id) as string;
  }

  update(id: string, opts: { status?: string; priority?: string; type?: string }): void {
    const args = ['update', id];
    if (opts.status) args.push('-s', opts.status);
    if (opts.priority) args.push('--priority', opts.priority);
    if (opts.type) args.push('-t', opts.type);
    this.run(args);
  }

  /** Run a GraphQL query against the running beans-serve instance. */
  async graphql<T = unknown>(query: string): Promise<T> {
    const res = await fetch(`${this.baseURL}/api/graphql`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query })
    });
    const json = await res.json();
    return json.data as T;
  }

  /** Get all worktree paths from the running server. */
  async getWorktrees(): Promise<{ id: string; path: string; branch: string }[]> {
    const data = await this.graphql<{ worktrees: { id: string; path: string; branch: string }[] }>(
      '{ worktrees { id path branch } }'
    );
    return data.worktrees;
  }

  /** Create a workspace (git worktree) via the GraphQL API. */
  async createWorktree(name: string): Promise<{ id: string; path: string; branch: string }> {
    const data = await this.graphql<{
      createWorktree: { id: string; path: string; branch: string };
    }>(`mutation { createWorktree(name: ${JSON.stringify(name)}) { id path branch } }`);
    return data.createWorktree;
  }

  /**
   * Run the beans CLI inside a worktree, against the worktree's own .beans/
   * directory — exactly like an agent working in that workspace would.
   */
  runInWorktree(worktreePath: string, args: string[]): string {
    return execFileSync(this.binaryPath, ['--beans-path', join(worktreePath, '.beans'), ...args], {
      cwd: worktreePath,
      encoding: 'utf-8',
      timeout: 10_000
    });
  }

  /** Run git in the given directory with a deterministic identity. */
  git(cwd: string, args: string[]): string {
    return execFileSync('git', args, { cwd, encoding: 'utf-8', timeout: 10_000, env: GIT_ENV });
  }

  /** Create a file in a worktree directory to simulate uncommitted changes. */
  createFileInWorktree(worktreePath: string, filename: string, content: string): void {
    writeFileSync(join(worktreePath, filename), content);
  }

  /** Create a commit in a worktree directory to simulate unmerged commits. */
  commitInWorktree(worktreePath: string, filename: string, content: string): void {
    writeFileSync(join(worktreePath, filename), content);
    execFileSync('git', ['add', filename], { cwd: worktreePath, timeout: 10_000 });
    execFileSync('git', ['commit', '-m', 'test commit'], {
      cwd: worktreePath,
      timeout: 10_000,
      env: GIT_ENV
    });
  }
}

type BeansDefaults = { status?: string; type?: string };

/** Set beans.default_status / beans.default_type in a `.beans.yml` document. */
function applyBeansDefaults(config: string, defaults: BeansDefaults): string {
  const lines = config.split('\n');
  const beansIdx = lines.indexOf('beans:');
  if (beansIdx === -1) throw new Error('.beans.yml has no top-level "beans:" section');
  const indent = /^(\s+)/.exec(lines[beansIdx + 1] ?? '')?.[1] ?? '  ';

  const set = (key: string, value: string | undefined) => {
    if (!value) return;
    const line = `${indent}${key}: ${value}`;
    const existing = lines.findIndex((l) => l.startsWith(`${indent}${key}:`));
    if (existing === -1) lines.splice(beansIdx + 1, 0, line);
    else lines[existing] = line;
  };
  set('default_status', defaults.status);
  set('default_type', defaults.type);
  return lines.join('\n');
}

type Fixtures = {
  /** Run command configured for `beansWithRun` (override with `test.use({ runCommand })`). */
  runCommand: string;
  beans: BeansCLI;
  beansWithRun: BeansCLI;
  backlogPage: BacklogPage;
  boardPage: BoardPage;
};

type WorkerFixtures = {
  beansTemplate: string;
};

/**
 * Each test gets its own temp directory (copied from a worker-scoped template),
 * beans-serve process, and port. Full isolation — no shared state between tests.
 */
export const test = base.extend<Fixtures, WorkerFixtures>({
  // A long-running process we can stop
  runCommand: ['sleep 300', { option: true }],

  // Worker-scoped: run `beans init` once per worker, copy .beans dir for each test.
  // Fresh git repos are created per test (avoids stale git index/worktree state).
  beansTemplate: [
    async ({}, use) => {
      const { beans: beansBin } = getBinaries();
      const templateDir = createBeansTemplate(beansBin);
      await use(templateDir);
      rmSync(templateDir, { recursive: true, force: true });
    },
    { scope: 'worker' }
  ],

  beansDefaults: [null, { option: true }],

  beans: async ({ page, beansTemplate, beansDefaults }, use, testInfo) => {
    const { beans: beansBin, beansServe } = getBinaries();

    // Fresh git repo per test (needed for worktree operations)
    const projectDir = mkdtempSync(join(tmpdir(), 'beans-e2e-'));
    const fakeClaudeDir = mkdtempSync(join(tmpdir(), 'beans-e2e-claude-'));
    execFileSync('git', ['init', '-b', 'main'], { cwd: projectDir, timeout: 10_000 });
    execFileSync('git', ['commit', '--allow-empty', '-m', 'init'], {
      cwd: projectDir,
      timeout: 10_000,
      env: GIT_ENV
    });
    // Copy pre-initialized beans files from template (avoids expensive `beans init` per test)
    cpSync(join(beansTemplate, '.beans'), join(projectDir, '.beans'), { recursive: true });
    cpSync(join(beansTemplate, '.beans.yml'), join(projectDir, '.beans.yml'));
    if (beansDefaults) {
      const configPath = join(projectDir, '.beans.yml');
      writeFileSync(
        configPath,
        applyBeansDefaults(readFileSync(configPath, 'utf-8'), beansDefaults)
      );
    }

    const beansPath = join(projectDir, '.beans');

    // Pick a unique port based on worker + test index
    const port = serverPort(testInfo.workerIndex, testInfo.parallelIndex);

    // Start beans-serve
    const server: ChildProcess = spawn(
      beansServe,
      ['--port', String(port), '--beans-path', beansPath],
      {
        cwd: projectDir,
        env: serverEnv(fakeClaudeDir),
        stdio: 'pipe'
      }
    );

    try {
      await waitForServer(port);

      // Set the base URL for this test's page
      await page.goto(`http://localhost:${port}/`);
      // Navigate away so tests start fresh with goto()
      await page.goto('about:blank');

      const cli = new BeansCLI(
        beansPath,
        projectDir,
        beansBin,
        `http://localhost:${port}`,
        fakeClaudeDir
      );
      await use(cli);
    } finally {
      server.kill();
      rmSync(projectDir, { recursive: true, force: true });
      rmSync(fakeClaudeDir, { recursive: true, force: true });
    }
  },

  beansWithRun: async ({ page, beansTemplate, runCommand }, use, testInfo) => {
    const { beans: beansBin, beansServe } = getBinaries();

    const projectDir = mkdtempSync(join(tmpdir(), 'beans-e2e-'));
    const fakeClaudeDir = mkdtempSync(join(tmpdir(), 'beans-e2e-claude-'));
    execFileSync('git', ['init', '-b', 'main'], { cwd: projectDir, timeout: 10_000 });
    execFileSync('git', ['commit', '--allow-empty', '-m', 'init'], {
      cwd: projectDir,
      timeout: 10_000,
      env: GIT_ENV
    });
    cpSync(join(beansTemplate, '.beans'), join(projectDir, '.beans'), { recursive: true });
    cpSync(join(beansTemplate, '.beans.yml'), join(projectDir, '.beans.yml'));

    // Add the run command to the config
    const configPath = join(projectDir, '.beans.yml');
    const config = readFileSync(configPath, 'utf-8');
    writeFileSync(configPath, config.replace(/run: ""/g, `run: ${JSON.stringify(runCommand)}`));

    const beansPath = join(projectDir, '.beans');
    const port = serverPort(testInfo.workerIndex, testInfo.parallelIndex);

    const server: ChildProcess = spawn(
      beansServe,
      ['--port', String(port), '--beans-path', beansPath],
      {
        cwd: projectDir,
        env: serverEnv(fakeClaudeDir),
        stdio: 'pipe'
      }
    );

    try {
      await waitForServer(port);
      await page.goto(`http://localhost:${port}/`);
      await page.goto('about:blank');

      const cli = new BeansCLI(
        beansPath,
        projectDir,
        beansBin,
        `http://localhost:${port}`,
        fakeClaudeDir
      );
      await use(cli);
    } finally {
      server.kill();
      rmSync(projectDir, { recursive: true, force: true });
      rmSync(fakeClaudeDir, { recursive: true, force: true });
    }
  },

  backlogPage: async ({ page, beans }, use) => {
    const backlog = new BacklogPage(page, beans.baseURL);
    await use(backlog);
  },

  boardPage: async ({ page, beans }, use) => {
    const board = new BoardPage(page, beans.baseURL);
    await use(board);
  }
});

export { expect } from '@playwright/test';
