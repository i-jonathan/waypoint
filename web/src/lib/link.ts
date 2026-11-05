import { navigate } from './router.svelte';

/**
 * Click handler for an in-app `<a href>`: a plain left click goes through the
 * router, while a modified click (new tab, download) is left to the browser.
 */
export function go(e: MouseEvent & { currentTarget: HTMLAnchorElement }): void {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  const a = e.currentTarget;
  if (a.target !== '' && a.target !== '_self') return;
  e.preventDefault();
  navigate(a.pathname + a.search);
}
