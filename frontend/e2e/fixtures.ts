import { test as base, type Page, type TestInfo } from '@playwright/test';
import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync
} from 'node:fs';
import { connect } from 'node:net';
import { tmpdir } from 'node:os';
import { delimiter, join } from 'node:path';
import { BacklogPage } from './pages/backlog-page';
import { BoardPage } from './pages/board-page';

/** Default first port for beans-serve instances (override: BEANS_E2E_BASE_PORT). */
const DEFAULT_BASE_PORT = 22900;
/** Every e2e run uses ports in [base, base + PORT_RANGE). */
const PORT_RANGE = 100;

function basePort(): number {
  const override = process.env.BEANS_E2E_BASE_PORT;
  if (!override) return DEFAULT_BASE_PORT;
  const port = Number(override);
  if (!Number.isInteger(port) || port <= 0 || port + PORT_RANGE > 65_536) {
    throw new Error(`invalid BEANS_E2E_BASE_PORT: ${override}`);
  }
  return port;
}

/**
 * Pick the port for a test's beans-serve instance.
 *
 * All servers of a run stay in [base, base + PORT_RANGE), which lets several
 * e2e runs share a machine (give each its own BEANS_E2E_BASE_PORT). Concurrent
 * workers always have distinct parallelIndex values (< workers), so their ports
 * never collide. Within that bound, the port also rotates with workerIndex —
 * which grows whenever Playwright replaces a crashed/failed worker — so a fresh
 * worker does not race the previous worker's (possibly still exiting) server.
 */
function serverPort(testInfo: TestInfo): number {
  const workers = Math.max(1, testInfo.config.workers);
  if (workers > PORT_RANGE) throw new Error(`at most ${PORT_RANGE} workers are supported`);
  const generations = Math.floor(PORT_RANGE / workers);
  return basePort() + (testInfo.workerIndex % generations) * workers + testInfo.parallelIndex;
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

/** Create a temp directory and return its canonical (symlink-free) path. */
function makeTempDir(prefix: string): string {
  return realpathSync(mkdtempSync(join(tmpdir(), prefix)));
}

/**
 * Create a beans template directory via `beans init`.
 * Called once per worker. Each test gets a fresh git repo but copies the
 * pre-initialized .beans directory to avoid the expensive CLI invocation.
 */
function createBeansTemplate(beansBin: string): string {
  const templateDir = makeTempDir('beans-e2e-template-');
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
 * Set keys of a top-level section in a `.beans.yml` document. Values must be
 * YAML scalars (already quoted where needed); undefined values are skipped.
 * Only keys inside `section` are matched, so e.g. `worktree.path` never
 * clobbers `beans.path`.
 */
function setYamlKeys(
  config: string,
  section: string,
  entries: Record<string, string | undefined>
): string {
  const lines = config.split('\n');
  let start = lines.indexOf(`${section}:`);
  if (start === -1) {
    lines.push(`${section}:`);
    start = lines.length - 1;
  }
  const sectionEnd = () => {
    const next = lines.findIndex((l, i) => i > start && /^\S/.test(l));
    return next === -1 ? lines.length : next;
  };
  const indent = /^(\s+)\S/.exec(lines[start + 1] ?? '')?.[1] ?? '  ';

  for (const [key, value] of Object.entries(entries)) {
    if (value === undefined) continue;
    const line = `${indent}${key}: ${value}`;
    const end = sectionEnd();
    const existing = lines.findIndex(
      (l, i) => i > start && i < end && l.startsWith(`${indent}${key}:`)
    );
    if (existing === -1) lines.splice(start + 1, 0, line);
    else lines[existing] = line;
  }
  return lines.join('\n');
}

/** worktree.backend of the test project (see CLAUDE.md "Worktree backends"). */
export type WorktreeBackend = 'git' | 'worktrunk';

/** An isolated on-disk test project: git repo, beans data, and worktree root. */
type TestProject = {
  projectDir: string;
  beansPath: string;
  /** State directory of the fake claude CLI ($FAKE_CLAUDE_DIR). */
  fakeClaudeDir: string;
  /** worktree.path — keeps test worktrees out of ~/.beans/worktrees. */
  worktreeDir: string;
  /** Isolated worktrunk setup (worktrunk backend only). */
  worktrunk: Worktrunk | null;
};

/**
 * Isolated worktrunk (wt) configuration: its own HOME, user config (with a
 * worktree-path template inside the project's worktree root), approvals file
 * and a nonexistent system config, so the developer's own worktrunk config,
 * hooks and approvals never apply. Mirrors setupWorktrunkRepo in
 * internal/worktree/worktrunk_integration_test.go.
 */
type Worktrunk = {
  dir: string;
  /** Environment variables for beans-serve and every `wt` invocation. */
  env: Record<string, string>;
};

/** Path of the worktrunk (wt) executable, or null when it isn't installed. */
export function findWorktrunk(): string | null {
  try {
    const out = execFileSync('sh', ['-c', 'command -v wt'], { encoding: 'utf-8', timeout: 5_000 });
    return out.trim() || null;
  } catch {
    return null;
  }
}

function createWorktrunk(worktreeDir: string): Worktrunk {
  const dir = makeTempDir('beans-e2e-wt-');
  const home = join(dir, 'home');
  mkdirSync(home, { recursive: true });
  const userConfig = join(dir, 'wt-user.toml');
  // Worktrees go to <worktreeDir>/trees/<branch>, inside the project's worktree
  // root, so removeTestProject's leak check covers them.
  writeFileSync(
    userConfig,
    `worktree-path = ${JSON.stringify(join(worktreeDir, 'trees', '{{ branch | sanitize }}'))}\n`
  );
  return {
    dir,
    env: {
      HOME: home,
      XDG_CONFIG_HOME: join(home, '.config'),
      WORKTRUNK_CONFIG_PATH: userConfig,
      WORKTRUNK_SYSTEM_CONFIG_PATH: join(dir, 'wt-system.toml'),
      WORKTRUNK_APPROVALS_PATH: join(dir, 'wt-approvals.toml'),
      GIT_CONFIG_NOSYSTEM: '1'
    }
  };
}

/**
 * Create a fresh git repo (needed for worktree operations; avoids stale git
 * index/worktree state) with beans files copied from the worker's template.
 * `configure` may edit the `.beans.yml` document.
 */
function createTestProject(
  beansTemplate: string,
  configure: (config: string) => string = (c) => c,
  backend: WorktreeBackend = 'git'
): TestProject {
  const projectDir = makeTempDir('beans-e2e-');
  const fakeClaudeDir = makeTempDir('beans-e2e-claude-');
  const worktreeDir = makeTempDir('beans-e2e-worktrees-');
  execFileSync('git', ['init', '-b', 'main'], { cwd: projectDir, timeout: 10_000 });
  execFileSync('git', ['commit', '--allow-empty', '-m', 'init'], {
    cwd: projectDir,
    timeout: 10_000,
    env: GIT_ENV
  });
  // Copy pre-initialized beans files from template (avoids expensive `beans init` per test)
  cpSync(join(beansTemplate, '.beans'), join(projectDir, '.beans'), { recursive: true });
  const configPath = join(projectDir, '.beans.yml');
  const config = setYamlKeys(readFileSync(join(beansTemplate, '.beans.yml'), 'utf-8'), 'worktree', {
    path: JSON.stringify(worktreeDir),
    backend: backend === 'worktrunk' ? 'worktrunk' : undefined
  });
  writeFileSync(configPath, configure(config));

  return {
    projectDir,
    beansPath: join(projectDir, '.beans'),
    fakeClaudeDir,
    worktreeDir,
    worktrunk: backend === 'worktrunk' ? createWorktrunk(worktreeDir) : null
  };
}

/** Paths of the project's git worktrees other than the main checkout. */
function linkedWorktrees(project: TestProject): string[] {
  const out = execFileSync('git', ['worktree', 'list', '--porcelain'], {
    cwd: project.projectDir,
    encoding: 'utf-8',
    timeout: 10_000
  });
  return out
    .split('\n')
    .filter((l) => l.startsWith('worktree '))
    .map((l) => l.slice('worktree '.length))
    .filter((p) => p !== project.projectDir);
}

/**
 * Remove all of the project's directories. Throws (after cleaning up) if any
 * worktree was created outside the project's worktree root, so a regression
 * that leaks worktrees into ~/.beans/worktrees fails the test.
 */
function removeTestProject(project: TestProject): void {
  let leaked: string[] = [];
  try {
    leaked = linkedWorktrees(project).filter((p) => !p.startsWith(project.worktreeDir + '/'));
  } finally {
    const dirs = [project.projectDir, project.fakeClaudeDir, project.worktreeDir, ...leaked];
    if (project.worktrunk) dirs.push(project.worktrunk.dir);
    for (const dir of dirs) {
      rmSync(dir, { recursive: true, force: true });
    }
  }
  if (leaked.length > 0) {
    throw new Error(`worktrees leaked outside ${project.worktreeDir}: ${leaked.join(', ')}`);
  }
}

/** Directory containing the fake `claude` CLI (see fake-claude/fake-claude.mjs). */
const FAKE_CLAUDE_BIN = join(import.meta.dirname, 'fake-claude');

/**
 * Environment for beans-serve. The fake `claude` shadows any real Claude Code
 * install on PATH, so agent sessions in e2e tests never start a real agent.
 */
function serverEnv(project: TestProject): NodeJS.ProcessEnv {
  return {
    ...process.env,
    GIN_MODE: 'release',
    PATH: `${FAKE_CLAUDE_BIN}${delimiter}${process.env.PATH ?? ''}`,
    FAKE_CLAUDE_DIR: project.fakeClaudeDir,
    ...project.worktrunk?.env
  };
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function isExited(proc: ChildProcess): boolean {
  return proc.exitCode !== null || proc.signalCode !== null;
}

/** Whether something accepts TCP connections on the port. */
function portInUse(port: number): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
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
}

/** Thrown when beans-serve exits before it starts serving. */
class ServerExitedError extends Error {}

/**
 * Wait for our server process to start accepting TCP connections, then verify
 * HTTP. Fails fast if the process exits (e.g. the port is taken), so we never
 * mistake another process on the same port for our server.
 */
async function waitForServer(port: number, server: ChildProcess, timeoutMs = 10_000) {
  const start = Date.now();
  const checkAlive = () => {
    if (isExited(server)) throw new ServerExitedError('beans-serve exited during startup');
  };
  // First, wait for TCP port to open (much faster than HTTP fetch for early polls)
  while (Date.now() - start < timeoutMs) {
    checkAlive();
    if (await portInUse(port)) break;
    await sleep(25);
  }
  // Then verify HTTP is actually ready
  while (Date.now() - start < timeoutMs) {
    checkAlive();
    try {
      const res = await fetch(`http://localhost:${port}/`);
      if (res.ok) {
        checkAlive();
        return;
      }
    } catch {
      // not ready yet
    }
    await sleep(50);
  }
  throw new Error(`Server on port ${port} did not start within ${timeoutMs}ms`);
}

/** Retained beans-serve output (for startup failure diagnostics). */
const MAX_SERVER_OUTPUT = 64 * 1024;

/**
 * Start beans-serve for a test project on the given port.
 *
 * The port is usually free, but a previous test's server may still be
 * shutting down (or another run may briefly hold it). We wait for the port to
 * be released and retry when the server fails with EADDRINUSE.
 */
async function startServer(
  beansServe: string,
  project: TestProject,
  port: number
): Promise<ChildProcess> {
  const deadline = Date.now() + 15_000;
  for (;;) {
    while ((await portInUse(port)) && Date.now() < deadline) await sleep(100);

    const server = spawn(beansServe, ['--port', String(port), '--beans-path', project.beansPath], {
      cwd: project.projectDir,
      env: serverEnv(project),
      stdio: ['ignore', 'pipe', 'pipe']
    });
    // Always drain output so a chatty server never blocks on a full pipe.
    let output = '';
    const collect = (chunk: Buffer) => {
      output = (output + chunk.toString()).slice(-MAX_SERVER_OUTPUT);
    };
    server.stdout?.on('data', collect);
    server.stderr?.on('data', collect);

    try {
      await waitForServer(port, server);
      return server;
    } catch (err) {
      await stopServer(server);
      const addrInUse = /address already in use/i.test(output);
      if (err instanceof ServerExitedError && addrInUse && Date.now() < deadline) {
        await sleep(200);
        continue;
      }
      throw new Error(`beans-serve failed to start on port ${port}: ${err}\n${output}`);
    }
  }
}

/** Stop beans-serve and wait until it has exited (and released its port). */
async function stopServer(server: ChildProcess, timeoutMs = 10_000): Promise<void> {
  if (isExited(server)) return;
  const exited = new Promise<void>((resolve) => server.once('exit', () => resolve()));
  server.kill('SIGTERM');
  const timedOut = await Promise.race([
    exited.then(() => false),
    sleep(timeoutMs).then(() => true)
  ]);
  if (timedOut && !isExited(server)) {
    server.kill('SIGKILL');
    await exited;
  }
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
    readonly fakeClaudeDir: string,
    /** worktree.path of the project (worktrunk worktrees live in <root>/trees/). */
    readonly worktreeRoot: string,
    /** Isolated worktrunk environment (worktrunk backend only). */
    private worktrunkEnv: Record<string, string> | null = null
  ) {}

  /**
   * Run worktrunk (wt) in the project with the same isolated configuration as
   * beans-serve, e.g. to create or remove worktrees "outside beans".
   */
  wt(args: string[]): string {
    if (!this.worktrunkEnv) throw new Error('wt() needs the worktrunk backend');
    return execFileSync('wt', args, {
      cwd: this.projectDir,
      encoding: 'utf-8',
      timeout: 30_000,
      env: { ...GIT_ENV, ...this.worktrunkEnv }
    });
  }

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

type Fixtures = {
  /** Run command configured for `beansWithRun` (override with `test.use({ runCommand })`). */
  runCommand: string;
  /**
   * worktree.backend for `beans` (override with `test.use({ worktreeBackend })`).
   * 'worktrunk' runs beans-serve with an isolated worktrunk config; tests
   * should skip when `findWorktrunk()` returns null.
   */
  worktreeBackend: WorktreeBackend;
  /** beans.default_status / beans.default_type for `beans` (override with `test.use`). */
  beansDefaults: BeansDefaults | null;
  beans: BeansCLI;
  beansWithRun: BeansCLI;
  backlogPage: BacklogPage;
  boardPage: BoardPage;
};

type WorkerFixtures = {
  beansTemplate: string;
};

/**
 * Run a test against its own isolated project (temp git repo, beans data and
 * worktree root) served by its own beans-serve instance on its own port.
 * Everything is stopped and removed on teardown — nothing is written to
 * ~/.beans.
 */
async function withServedProject(
  page: Page,
  beansTemplate: string,
  testInfo: TestInfo,
  configure: (config: string) => string,
  use: (cli: BeansCLI) => Promise<void>,
  backend: WorktreeBackend = 'git'
): Promise<void> {
  const { beans: beansBin, beansServe } = getBinaries();
  const project = createTestProject(beansTemplate, configure, backend);
  let server: ChildProcess | undefined;
  try {
    const port = serverPort(testInfo);
    server = await startServer(beansServe, project, port);

    // Set the base URL for this test's page
    await page.goto(`http://localhost:${port}/`);
    // Navigate away so tests start fresh with goto()
    await page.goto('about:blank');

    await use(
      new BeansCLI(
        project.beansPath,
        project.projectDir,
        beansBin,
        `http://localhost:${port}`,
        project.fakeClaudeDir,
        project.worktreeDir,
        project.worktrunk?.env ?? null
      )
    );
  } finally {
    if (server) await stopServer(server);
    removeTestProject(project);
  }
}

/**
 * Each test gets its own temp directory (copied from a worker-scoped template),
 * beans-serve process, and port. Full isolation — no shared state between tests.
 */
export const test = base.extend<Fixtures, WorkerFixtures>({
  // A long-running process we can stop
  runCommand: ['sleep 300', { option: true }],

  beansDefaults: [null, { option: true }],

  worktreeBackend: ['git', { option: true }],

  // Worker-scoped: run `beans init` once per worker, copy .beans dir for each test.
  beansTemplate: [
    async ({}, use) => {
      const { beans: beansBin } = getBinaries();
      const templateDir = createBeansTemplate(beansBin);
      await use(templateDir);
      rmSync(templateDir, { recursive: true, force: true });
    },
    { scope: 'worker' }
  ],

  beans: async ({ page, beansTemplate, beansDefaults, worktreeBackend }, use, testInfo) => {
    await withServedProject(
      page,
      beansTemplate,
      testInfo,
      (config) =>
        beansDefaults
          ? setYamlKeys(config, 'beans', {
              default_status: beansDefaults.status,
              default_type: beansDefaults.type
            })
          : config,
      use,
      worktreeBackend
    );
  },

  beansWithRun: async ({ page, beansTemplate, runCommand }, use, testInfo) => {
    await withServedProject(
      page,
      beansTemplate,
      testInfo,
      (config) => setYamlKeys(config, 'worktree', { run: JSON.stringify(runCommand) }),
      use
    );
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
