/**
 * Pure helpers for workspaces (git worktrees): name validation and display.
 * The reactive store lives in worktrees.svelte.ts.
 */

/** Maximum workspace name length in bytes (internal/worktree/name.go maxNameLength). */
export const MAX_WORKSPACE_NAME_LENGTH = 100;

const VALID_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export interface NameValidation {
  /** Why the name is invalid, e.g. "it must not contain '..'". */
  reason: string;
  /** A valid alternative derived from the input, if there is one. */
  suggestion: string | null;
}

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

/**
 * Turn arbitrary text into a valid workspace name ("Run Test" -> "run-test"),
 * or "" if nothing usable remains. Mirrors worktree.Slugify.
 */
export function slugifyWorkspaceName(s: string): string {
  let slug = s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  if (slug.length > MAX_WORKSPACE_NAME_LENGTH) {
    slug = slug.slice(0, MAX_WORKSPACE_NAME_LENGTH).replace(/-+$/, '');
  }
  return slug;
}

/**
 * Check a workspace name with the backend's rules (worktree.ValidateName):
 * the name becomes the worktree ID, a directory name and the git branch, so it
 * must be a single path segment that is a valid branch name. Returns null when
 * the name is valid.
 */
export function validateWorkspaceName(name: string): NameValidation | null {
  let reason: string;
  if (name === '') {
    reason = 'it must not be empty';
  } else if (byteLength(name) > MAX_WORKSPACE_NAME_LENGTH) {
    reason = `it must be at most ${MAX_WORKSPACE_NAME_LENGTH} characters long`;
  } else if (!VALID_NAME.test(name)) {
    reason =
      "it must start with a letter or digit and contain only letters, digits, '.', '_' and '-'";
  } else if (name.includes('..')) {
    reason = "it must not contain '..'";
  } else if (name.endsWith('.') || name.endsWith('.lock')) {
    reason = "it must not end with '.' or '.lock'";
  } else {
    return null;
  }
  const slug = slugifyWorkspaceName(name);
  return { reason, suggestion: slug !== '' && slug !== name ? slug : null };
}

/** The parts of a worktree the display helpers need. */
export interface WorkspaceInfo {
  id: string;
  name?: string | null;
  branch: string;
  path: string;
}

/** A detached worktree has no branch checked out. */
export function isDetached(wt: WorkspaceInfo): boolean {
  return wt.branch === '';
}

/** Last path segment of a filesystem path. */
function baseName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? '';
}

/**
 * Label of a workspace: its name, else its branch, else (detached) the
 * worktree's directory name, else its ID.
 */
export function workspaceLabel(wt: WorkspaceInfo): string {
  return wt.name || wt.branch || baseName(wt.path) || wt.id;
}

/** Multi-line tooltip with the workspace's branch and path. */
export function workspaceTooltip(
  wt: WorkspaceInfo,
  extra: (string | null | undefined)[] = []
): string {
  const lines = [workspaceLabel(wt)];
  lines.push(isDetached(wt) ? 'Detached HEAD (no branch)' : `Branch: ${wt.branch}`);
  if (wt.path) lines.push(`Path: ${wt.path}`);
  for (const line of extra) if (line) lines.push(line);
  return lines.join('\n');
}
