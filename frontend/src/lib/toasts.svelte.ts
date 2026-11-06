/** App-wide toast notifications (rendered by Toaster.svelte in the root layout). */

export type ToastKind = 'info' | 'success' | 'error';

export interface Toast {
  id: number;
  kind: ToastKind;
  title: string;
  /** Optional details; `backticked` commands are rendered copyable. */
  message?: string;
}

export interface ToastOptions {
  kind?: ToastKind;
  message?: string;
  /** Auto-dismiss after this many ms; 0 keeps the toast until dismissed. Defaults: 6s, errors stay. */
  timeoutMs?: number;
}

class ToastStore {
  toasts = $state<Toast[]>([]);
  #nextId = 1;

  show(title: string, opts: ToastOptions = {}): number {
    const kind = opts.kind ?? 'info';
    const id = this.#nextId++;
    this.toasts = [...this.toasts, { id, kind, title, message: opts.message }];
    const timeout = opts.timeoutMs ?? (kind === 'error' ? 0 : 6000);
    if (timeout > 0) setTimeout(() => this.dismiss(id), timeout);
    return id;
  }

  error(title: string, message?: string): number {
    return this.show(title, { kind: 'error', message });
  }

  dismiss(id: number): void {
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }
}

export const toasts = new ToastStore();
