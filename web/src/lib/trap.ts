const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Keeps Tab inside a node while `enabled`: from the last control it goes to the
 * first, and from the first back to the last. A Svelte action.
 */
export function trapFocus(node: HTMLElement, enabled: boolean = true) {
  let on = enabled;

  function onKeydown(e: KeyboardEvent) {
    if (!on || e.key !== 'Tab') return;
    const items = [...node.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !el.hidden && el.tabIndex >= 0);
    if (items.length === 0) {
      e.preventDefault();
      node.focus();
      return;
    }
    const first = items[0]!;
    const last = items[items.length - 1]!;
    const here = document.activeElement;
    if (e.shiftKey && (here === first || here === node)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && here === last) {
      e.preventDefault();
      first.focus();
    } else if (!node.contains(here)) {
      e.preventDefault();
      first.focus();
    }
  }

  node.addEventListener('keydown', onKeydown);
  return {
    update(next: boolean) {
      on = next;
    },
    destroy() {
      node.removeEventListener('keydown', onKeydown);
    },
  };
}
