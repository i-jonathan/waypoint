<script lang="ts">
  import Plus from 'phosphor-svelte/lib/Plus';
  import type { Snippet } from 'svelte';
  import type { CardView, Column } from '../lib/types';
  import Card from './Card.svelte';

  interface Props {
    /**
     * `full` is a column with its sign, the board without swimlanes. In swimlanes
     * the signs are one row on top (`sign`) and each lane holds a `cell`.
     */
    part: 'full' | 'sign' | 'cell';
    column: Column;
    /** The cards to show, in rank order. A filter may hide some. */
    cards: readonly CardView[];
    /** The cards the column has on the whole board, which a filter does not change. */
    total: number;
    /** How many of them show, when the sign is drawn apart from the cards (swimlanes). */
    shown?: number;
    /** The id the drop zone is known by, and the lane it belongs to. */
    zone?: string;
    lane?: string;
    colors?: ReadonlyMap<string, string>;
    /** The priority drawn as the highest. */
    topPriority?: string;
    tabbableId?: string | null;
    dragId?: string | null;
    /** While a card is up: why this column cannot take it, or null. */
    refusal?: string | null;
    /** While a card is up and the pointer is over this zone, which cannot take it: say why. */
    explain?: boolean;
    /** While a card is up and over this zone: where the placeholder goes, in pixels from the top of the list. */
    slot?: number | null;
    readonly?: boolean;
    onAdd?: (column: string) => void;
    /** Takes the place of the Add card row, as quick add does while it is open. */
    footer?: Snippet<[string]>;
    onOpen: (id: string) => void;
    onPointerDown: (e: PointerEvent, id: string, el: HTMLElement) => void;
  }

  let {
    part,
    column,
    cards,
    total,
    shown,
    zone,
    lane,
    colors,
    topPriority = '',
    tabbableId = null,
    dragId = null,
    refusal = null,
    explain = false,
    slot = null,
    readonly = false,
    onAdd,
    footer,
    onOpen,
    onPointerDown,
  }: Props = $props();

  const wip = $derived(column.wip > 0);
  const visible = $derived(shown ?? cards.length);
  const filtered = $derived(visible !== total);
  const dragging = $derived(dragId !== null);
  const count = $derived(
    wip ? `${total}/${column.wip}` : filtered ? `${visible} of ${total}` : String(total),
  );
  const countLabel = $derived(
    (wip ? `${total} of ${column.wip} allowed` : `${total} ${total === 1 ? 'card' : 'cards'}`) +
      (filtered ? `, ${visible} shown` : ''),
  );
</script>

<div
  role="group"
  class="col"
  class:refused={refusal !== null}
  class:cell={part === 'cell'}
  data-zone={part === 'sign' ? undefined : zone}
  data-column={part === 'sign' ? undefined : column.name}
  data-lane={part === 'sign' ? undefined : lane}
  aria-label={part === 'sign' ? undefined : lane === undefined ? column.name : `${column.name}, ${lane === '' ? 'no lane' : lane}`}
  title={refusal ?? undefined}
>
  {#if part !== 'cell'}
    <h2 class="sign head">
      <span class="name">{column.name}</span>
      <span class="count mono" class:wip title={countLabel} aria-label={countLabel}>{count}</span>
    </h2>
  {/if}

  {#if part !== 'sign'}
    <div class="list" data-list data-scroll-y={part === 'full' ? '' : undefined}>
      {#if slot !== null}
        <div class="slot" style:transform="translateY({slot - 2}px)" aria-hidden="true"></div>
      {/if}
      {#if refusal !== null && explain}
        <p class="why">{refusal}</p>
      {/if}
      <ul>
        {#each cards as card (card.id)}
          <li>
            <Card
              {card}
              done={column.done}
              top={card.priority !== '' && card.priority === topPriority}
              color={colors?.get(card.group)}
              tabbable={card.id === tabbableId}
              dragging={card.id === dragId}
              {onOpen}
              {onPointerDown}
            />
          </li>
        {/each}
      </ul>
      {#if cards.length === 0 && refusal === null}
        <p class="empty">
          {#if dragging}Drop here{:else if total > 0}No matching cards{:else}Nothing here yet{/if}
        </p>
      {/if}
      {#if part === 'full' && footer}
        {@render footer(column.name)}
      {:else if part === 'full' && onAdd}
        <button class="add" disabled={readonly} onclick={() => onAdd(column.name)}>
          <Plus size={13} weight="bold" aria-hidden="true" />Add card
          <span class="sr-only"> to {column.name}</span>
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .col {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .col.cell {
    min-height: 56px;
  }

  .head {
    display: flex;
    align-items: center;
    flex: none;
    margin: 0;
    padding: 7px 11px 6px;
    border-radius: var(--r-sign);
    background: var(--head);
    color: var(--headtext);
    font-size: 22px;
    letter-spacing: 0.01em;
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    margin-left: auto;
    padding-left: 8px;
    font-size: 11.5px;
    font-weight: 500;
    font-stretch: 100%;
    letter-spacing: 0;
    text-transform: none;
    opacity: 0.75;
    white-space: nowrap;
  }

  /* An accent fill with its own ink, which is readable on the head colour in every theme and preset. */
  .count.wip {
    margin-right: -3px;
    padding: 1px 6px;
    border-radius: var(--r-chip);
    background: var(--acc);
    color: var(--acc-ink);
    font-weight: 600;
    opacity: 1;
  }

  .list {
    position: relative;
    flex: 1;
    min-height: 0;
    /* Room for the NEXT tag above the first card, the hard edge below and beside, and the focus ring. */
    margin: 0 -6px;
    padding: 12px 6px 6px;
  }

  .list[data-scroll-y] {
    overflow-y: auto;
    overscroll-behavior-y: contain;
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  /* A column that cannot take the card in hand is dimmed; its reason stays at full strength. */
  .col.refused .head,
  .col.refused ul,
  .col.refused .empty,
  .col.refused .add {
    opacity: 0.4;
  }

  .slot {
    position: absolute;
    top: 0;
    left: 6px;
    right: 9px;
    height: 4px;
    border-radius: 2px;
    background: var(--acc);
    pointer-events: none;
    z-index: 2;
  }

  .empty {
    margin: 0;
    padding: 10px 4px;
    color: var(--muted);
    font-size: 12px;
    line-height: 1.4;
  }

  /* Over the column, so that showing it moves nothing: the zones were measured when the drag began. */
  .why {
    position: absolute;
    top: 12px;
    left: 6px;
    right: 9px;
    z-index: 2;
    margin: 0;
    padding: 6px 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    color: var(--text);
    font-size: 12px;
    line-height: 1.35;
    pointer-events: none;
  }

  .empty {
    border: 1px dashed var(--line);
    border-radius: var(--r-card);
    text-align: center;
    margin-right: 3px;
  }

  .add {
    display: flex;
    gap: 6px;
    align-items: center;
    width: calc(100% - 3px);
    padding: 7px 12px;
    border: 1px dashed var(--line);
    border-radius: var(--r-card);
    background: none;
    color: var(--muted);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
  }

  .add:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--muted);
  }

  .add:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
