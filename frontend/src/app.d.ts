// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
  namespace App {
    // interface Error {}
    // interface Locals {}
    // interface PageData {}
    interface PageState {
      /** Set on the history entry focus mode added (see focusMode.svelte.ts), so exiting can go back. */
      focusEntered?: boolean;
    }
    // interface Platform {}
  }
}

export {};
