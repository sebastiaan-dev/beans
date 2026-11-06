import { client } from './graphqlClient';
import {
  FileChangesDocument,
  AllFileChangesDocument,
  BranchStatusDocument,
  type FileChangeFieldsFragment,
  type BranchStatusQuery
} from './graphql/generated';

export type FileChange = FileChangeFieldsFragment;

export type BranchStatus = BranchStatusQuery['branchStatus'];

class ChangesStore {
  changes = $state<FileChange[]>([]);
  allChanges = $state<FileChange[]>([]);
  branchStatus = $state<BranchStatus>({ commitsBehind: 0, hasConflicts: false });
  loading = $state(false);
  #intervalId: ReturnType<typeof setInterval> | null = null;
  #currentPath: string | null = null;

  async fetch(path?: string): Promise<void> {
    const p = path ?? null;
    const [result, allResult, branchResult] = await Promise.all([
      client.query(FileChangesDocument, { path: p }).toPromise(),
      client.query(AllFileChangesDocument, { path: p }).toPromise(),
      client.query(BranchStatusDocument, { path: p }).toPromise()
    ]);

    if (result.error) {
      console.error('Failed to fetch file changes:', result.error);
    } else {
      this.changes = result.data?.fileChanges ?? [];
    }

    if (allResult.error) {
      console.error('Failed to fetch all file changes:', allResult.error);
    } else {
      this.allChanges = allResult.data?.allFileChanges ?? [];
    }

    if (branchResult.error) {
      console.error('Failed to fetch branch status:', branchResult.error);
    } else if (branchResult.data?.branchStatus) {
      this.branchStatus = branchResult.data.branchStatus;
    }
  }

  startPolling(path?: string, intervalMs = 3000): void {
    this.stopPolling();
    this.#currentPath = path ?? null;
    this.fetch(path);
    this.#intervalId = setInterval(() => this.fetch(this.#currentPath ?? undefined), intervalMs);
  }

  stopPolling(): void {
    if (this.#intervalId) {
      clearInterval(this.#intervalId);
      this.#intervalId = null;
    }
  }
}

export const changesStore = new ChangesStore();
