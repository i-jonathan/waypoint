import type { Look } from './theme';

// A look being tried on while a settings form is open. App.svelte shows it over the
// saved look; it is never saved, and it goes when the form is cancelled or left.

let value = $state.raw<Look | null>(null);

export const preview = {
  get look(): Look | null {
    return value;
  },
  set(look: Look): void {
    value = look;
  },
  clear(): void {
    value = null;
  },
};
