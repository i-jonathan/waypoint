<script lang="ts">
  import { onMount } from 'svelte';
  import ArrowLineDown from 'phosphor-svelte/lib/ArrowLineDown';
  import ArrowLineUp from 'phosphor-svelte/lib/ArrowLineUp';
  import ArrowRight from 'phosphor-svelte/lib/ArrowRight';
  import Rows from 'phosphor-svelte/lib/Rows';

  interface Props {
    /** The card being moved, for the menu's name. */
    cardId: string;
    /** The other columns it may move to; the ones it may not are not listed. */
    columns: readonly string[];
    /** In swimlanes, the other lanes the card may go to (its own is not listed). */
    lanes?: ReadonlyArray<{ key: string; name: string }>;
    /** Where the card is on screen, in viewport pixels. */
    anchor: { left: number; top: number };
    onColumn: (column: string) => void;
    /** Changes the card's lane (its group or priority) and leaves it in its place. */
    onLane?: (key: string) => void;
    onTop: () => void;
    onBottom: () => void;
    /** Closes the menu; focus goes back to the card. */
    onClose: () => void;
  }

  let { cardId, columns, lanes = [], anchor, onColumn, onLane, onTop, onBottom, onClose }: Props = $props();

  let menu: HTMLDivElement | undefined = $state();

  const WIDTH = 224;
  const left = $derived(Math.max(8, Math.min(anchor.left + 12, (typeof innerWidth === 'number' ? innerWidth : 1024) - WIDTH - 8)));
  const top = $derived(Math.max(8, Math.min(anchor.top + 36, (typeof innerHeight === 'number' ? innerHeight : 768) - (columns.length + lanes.length + 2) * 34 - 24)));

  function items(): HTMLButtonElement[] {
    return menu ? [...menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')] : [];
  }

  onMount(() => {
    items()[0]?.focus();
  });

  function onKeydown(e: KeyboardEvent) {
    const list = items();
    const at = list.indexOf(document.activeElement as HTMLButtonElement);
    let next: number | null = null;
    if (e.key === 'ArrowDown') next = (at + 1) % list.length;
    else if (e.key === 'ArrowUp') next = (at - 1 + list.length) % list.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = list.length - 1;
    else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
      return;
    } else if (e.key === 'Tab') {
      onClose();
      return;
    } else return;
    e.preventDefault();
    e.stopPropagation();
    list[next]?.focus();
  }
</script>

<button class="scrim" tabindex="-1" aria-label="Close the move menu" onclick={onClose}></button>

<div
  class="menu"
  role="menu"
  aria-label="Move {cardId}"
  tabindex="-1"
  bind:this={menu}
  style:left="{left}px"
  style:top="{top}px"
  onkeydown={onKeydown}
>
  {#each columns as column (column)}
    <button role="menuitem" tabindex="-1" onclick={() => onColumn(column)}>
      <ArrowRight size={14} weight="bold" aria-hidden="true" />
      <span>Move to {column}</span>
    </button>
  {/each}
  {#each lanes as lane (lane.key)}
    <button role="menuitem" tabindex="-1" onclick={() => onLane?.(lane.key)}>
      <Rows size={14} weight="bold" aria-hidden="true" />
      <span>Move to lane: {lane.name}</span>
    </button>
  {/each}
  {#if columns.length + lanes.length > 0}<div class="rule" role="separator"></div>{/if}
  <button role="menuitem" tabindex="-1" onclick={onTop}>
    <ArrowLineUp size={14} weight="bold" aria-hidden="true" />
    <span>Top of column</span>
  </button>
  <button role="menuitem" tabindex="-1" onclick={onBottom}>
    <ArrowLineDown size={14} weight="bold" aria-hidden="true" />
    <span>Bottom of column</span>
  </button>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 70;
    border: 0;
    background: transparent;
    cursor: default;
  }

  .menu {
    position: fixed;
    z-index: 71;
    display: flex;
    flex-direction: column;
    width: 224px;
    padding: 5px;
    border: 1.5px solid var(--edge);
    border-radius: var(--r-control);
    background: var(--card);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .menu:focus {
    outline: none;
  }

  button[role='menuitem'] {
    display: flex;
    gap: 8px;
    align-items: center;
    min-height: 32px;
    padding: 6px 9px;
    border: 0;
    border-radius: 7px;
    background: none;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  button[role='menuitem']:hover,
  button[role='menuitem']:focus-visible {
    background: var(--acc);
    color: var(--acc-ink);
  }

  /* The ring is drawn inside: the menu's own edge would cut it. */
  button[role='menuitem']:focus-visible {
    outline-offset: -3px;
    outline-color: var(--acc-ink);
    box-shadow: none;
  }

  .rule {
    height: 1px;
    margin: 4px 6px;
    background: var(--line);
  }
</style>
