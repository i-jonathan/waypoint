<script lang="ts">
  import CaretDown from 'phosphor-svelte/lib/CaretDown';
  import type { DragState } from '../lib/dnd';
  import { zoneId, type Lane, type LaneMode, type Layout } from '../lib/lanes';
  import type { Column as ColumnT } from '../lib/types';
  import Column from './Column.svelte';

  interface Props {
    mode: LaneMode;
    layout: Layout;
    columns: readonly ColumnT[];
    /** The cards each column has on the whole board, for the signs. */
    totals: ReadonlyMap<string, number>;
    /** Done and total cards of each lane on the whole board, by lane key. */
    stats: ReadonlyMap<string, { done: number; total: number }>;
    collapsed: ReadonlySet<string>;
    colors: ReadonlyMap<string, string>;
    topPriority: string;
    tabbableId: string | null;
    dragId: string | null;
    drag: DragState | null;
    readonly: boolean;
    onToggle: (key: string) => void;
    onOpen: (id: string) => void;
    onPointerDown: (e: PointerEvent, id: string, el: HTMLElement) => void;
  }

  let { mode, layout, columns, totals, stats, collapsed, colors, topPriority, tabbableId, dragId, drag, readonly, onToggle, onOpen, onPointerDown }: Props =
    $props();

  /** How many of each column's cards show, across every lane. */
  const shownIn = $derived.by(() => {
    const m = new Map<string, number>();
    for (const col of columns) {
      let n = 0;
      for (const lane of layout.lanes) n += layout.cells.get(zoneId(col.name, lane.key))?.length ?? 0;
      m.set(col.name, n);
    }
    return m;
  });

  function summary(lane: Lane): string {
    const s = stats.get(lane.key);
    if (!s || s.total === 0) return 'No cards';
    return `${s.done} of ${s.total} done`;
  }
</script>

<div class="rows" style:--n={columns.length}>
  <div class="row signs">
    {#each columns as col (col.name)}
      <Column part="sign" column={col} cards={[]} total={totals.get(col.name) ?? 0} shown={shownIn.get(col.name) ?? 0} {onOpen} {onPointerDown} />
    {/each}
  </div>

  {#each layout.lanes as lane (lane.key)}
    {@const open = !collapsed.has(lane.key)}
    <div class="lane" role="group" aria-label={lane.name}>
      <h3 class="lh">
        <button aria-expanded={open} onclick={() => onToggle(lane.key)}>
          <CaretDown class="caret" size={14} weight="bold" aria-hidden="true" />
          {#if lane.color}<span class="route" style:background={lane.color} aria-hidden="true"></span>{/if}
          <span class="lname" class:none={lane.none}>{lane.name}</span>
          <span class="stat mono">{summary(lane)}</span>
        </button>
      </h3>
      {#if open}
        <div class="row">
          {#each columns as col (col.name)}
            {@const zone = zoneId(col.name, lane.key)}
            {@const cards = layout.cells.get(zone) ?? []}
            <Column
              part="cell"
              column={col}
              {cards}
              total={cards.length}
              {zone}
              lane={lane.key}
              {colors}
              {topPriority}
              {tabbableId}
              {dragId}
              refusal={drag?.blocked.get(col.name) ?? null}
              explain={drag?.over === zone}
              slot={drag?.target?.zone === zone ? drag.target.slotY : null}
              {readonly}
              {onOpen}
              {onPointerDown}
            />
          {/each}
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .rows {
    --track: calc(var(--n) * var(--col-min) + (var(--n) - 1) * var(--col-gap));
    min-width: var(--track);
  }

  .row {
    display: grid;
    grid-template-columns: repeat(var(--n), minmax(var(--col-min), 1fr));
    gap: var(--col-gap);
  }

  /* The signs stay in view while the lanes scroll under them. */
  .signs {
    position: sticky;
    top: 0;
    z-index: 5;
    padding-bottom: 6px;
    background: var(--bg);
  }

  .lane {
    padding-top: 6px;
  }

  .lh {
    position: sticky;
    left: 0;
    z-index: 1;
    width: fit-content;
    max-width: 100%;
    margin: 0 0 2px;
    font-size: 13px;
  }

  .lh button {
    display: flex;
    gap: 8px;
    align-items: center;
    min-height: 32px;
    padding: 4px 10px 4px 6px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-weight: 600;
    cursor: pointer;
  }

  .lh button:hover {
    border-color: var(--muted);
  }

  .lh :global(.caret) {
    flex: none;
    color: var(--muted);
    transition: transform 150ms ease-out;
  }

  .lh button[aria-expanded='false'] :global(.caret) {
    transform: rotate(-90deg);
  }

  .route {
    flex: none;
    width: 16px;
    height: 4px;
    border-radius: 2px;
  }

  .lname.none {
    color: var(--muted);
    font-weight: 500;
  }

  .stat {
    font-size: 11px;
    font-weight: 400;
    color: var(--muted);
  }
</style>
