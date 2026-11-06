import { client } from './graphqlClient';
import { ConfigDocument } from './graphql/generated';

class ConfigStore {
  projectName = $state('');
  /** Default status/type for new beans (beans.default_status / default_type). */
  defaultBeanStatus = $state('todo');
  defaultBeanType = $state('task');
  mainBranch = $state('main');
  agentEnabled = $state(true);
  worktreeBaseRef = $state('main');
  worktreeRunCommand = $state('');
  worktreeIntegrateMode = $state<'local' | 'pr'>('local');
  /** worktree.backend: 'worktrunk' means workspaces are managed with worktrunk (wt). */
  worktreeBackend = $state<'git' | 'worktrunk'>('git');

  get isWorktrunk(): boolean {
    return this.worktreeBackend === 'worktrunk';
  }

  /** Whether the config has been loaded (until then the fields hold defaults). */
  loaded = $state(false);
  #resolveReady!: () => void;
  /** Resolves once load() has finished (successfully or not). */
  readonly ready: Promise<void> = new Promise((resolve) => (this.#resolveReady = resolve));

  async load(): Promise<void> {
    try {
      await this.#load();
    } finally {
      this.#resolveReady();
    }
  }

  async #load(): Promise<void> {
    const result = await client.query(ConfigDocument, {}).toPromise();
    if (result.error) {
      console.warn('Failed to load config:', result.error.message);
      return;
    }
    if (result.data) {
      this.projectName = result.data.projectName;
      this.defaultBeanStatus = result.data.defaultBeanStatus;
      this.defaultBeanType = result.data.defaultBeanType;
      this.mainBranch = result.data.mainBranch;
      this.agentEnabled = result.data.agentEnabled;
      this.worktreeBaseRef = result.data.worktreeBaseRef;
      this.worktreeRunCommand = result.data.worktreeRunCommand;
      this.worktreeIntegrateMode = result.data.worktreeIntegrateMode as 'local' | 'pr';
      this.worktreeBackend = result.data.worktreeBackend === 'worktrunk' ? 'worktrunk' : 'git';
      this.loaded = true;
    }
  }
}

export const configStore = new ConfigStore();
