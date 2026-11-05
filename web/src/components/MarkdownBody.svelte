<script lang="ts">
  import { renderBody } from '../lib/markdown';

  interface Props {
    body: string;
    /** Checkboxes cannot be ticked. */
    disabled?: boolean;
    /** A checkbox was clicked: the nth item of the card's checklist. */
    onCheck: (n: number) => void;
  }

  let { body, disabled = false, onCheck }: Props = $props();

  let el: HTMLDivElement | undefined = $state();

  // The page is rebuilt from the body each time it changes, so a ticked box must hand its focus on.
  $effect(() => {
    const fragment = renderBody(body, { disabled });
    if (!el) return;
    const active = document.activeElement;
    const keep = active instanceof HTMLInputElement && el.contains(active) ? active.dataset.n : undefined;
    el.replaceChildren(fragment);
    if (keep !== undefined) el.querySelector<HTMLInputElement>(`input[data-n="${keep}"]`)?.focus();
  });

  function onClick(e: MouseEvent) {
    const t = e.target;
    if (!(t instanceof HTMLInputElement) || t.dataset.n === undefined) return;
    // The box shows the card, not the click: it is redrawn from the body once the tick is in.
    e.preventDefault();
    if (!disabled) onCheck(Number(t.dataset.n));
  }
</script>

<!-- The checkboxes inside are real inputs, so keys reach them without help. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="md" bind:this={el} onclick={onClick}></div>

<style>
  .md {
    font-size: 14px;
    line-height: 1.55;
    overflow-wrap: anywhere;
  }

  .md :global(> :first-child) {
    margin-top: 0;
  }

  .md :global(> :last-child) {
    margin-bottom: 0;
  }

  .md :global(p),
  .md :global(ul),
  .md :global(ol),
  .md :global(blockquote),
  .md :global(pre),
  .md :global(table) {
    margin: 0 0 10px;
  }

  .md :global(h1),
  .md :global(h2),
  .md :global(h3),
  .md :global(h4),
  .md :global(h5),
  .md :global(h6) {
    margin: 14px 0 6px;
    font-size: 14px;
    font-weight: 600;
    line-height: 1.35;
  }

  .md :global(h1) {
    font-size: 15px;
  }

  .md :global(ul),
  .md :global(ol) {
    padding-left: 22px;
  }

  .md :global(li:has(> input[type='checkbox'])),
  .md :global(li:has(> p > input[type='checkbox'])) {
    list-style: none;
    margin-left: -22px;
  }

  .md :global(input[type='checkbox']) {
    width: 15px;
    height: 15px;
    margin: 0 8px 0 0;
    vertical-align: -2px;
    accent-color: var(--acc);
    cursor: pointer;
  }

  .md :global(input[type='checkbox']:disabled) {
    cursor: not-allowed;
  }

  .md :global(a) {
    color: var(--acc);
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  .md :global(a:not([href])) {
    color: inherit;
    text-decoration: none;
  }

  .md :global(code) {
    padding: 1px 5px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    font-family: var(--font-mono);
    font-size: 12.5px;
  }

  .md :global(pre) {
    overflow-x: auto;
    padding: 9px 11px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: color-mix(in srgb, var(--bg) 60%, var(--card));
  }

  .md :global(pre code) {
    padding: 0;
    border: 0;
    font-size: 12.5px;
  }

  .md :global(blockquote) {
    padding-left: 12px;
    border-left: 3px solid var(--line);
    color: var(--muted);
  }

  .md :global(hr) {
    border: 0;
    border-top: 1px solid var(--line);
    margin: 12px 0;
  }

  .md :global(table) {
    border-collapse: collapse;
    font-size: 13px;
  }

  .md :global(th),
  .md :global(td) {
    padding: 4px 9px;
    border: 1px solid var(--line);
    text-align: left;
  }
</style>
