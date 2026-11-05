<script lang="ts">
  import { tick, untrack } from 'svelte';
  import type { BoardStore } from '../lib/board.svelte';
  import { groupColor } from '../lib/colors';
  import { toasts } from '../lib/events.svelte';
  import { createDrag, type DragState, type DropTarget } from '../lib/dnd';
  import { canDrop, columnIds, dropIndex } from '../lib/moveRules';
  import { adjacentColumn, focusTarget, readKey, stepTarget, type NavGrid } from '../lib/keys';
  import { createLayout, laneKeyOf, loadCollapsed, saveCollapsed, zoneId, type LaneMode, type Layout } from '../lib/lanes';
  import { href, navigate } from '../lib/router.svelte';
  import type { Swimlane } from '../lib/types';
  import Column from './Column.svelte';
  import MoveMenu from './MoveMenu.svelte';
  import Swimlanes from './Swimlanes.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    lane: Swimlane;
    /** The Add card row of a column. Quick add arrives with the card panel. */
    onAddCard?: (column: string) => void;
  }

  let { store, project, lane, onAddCard }: Props = $props();

  const view = $derived(store.view);
  // The whole board, whatever the filter: every index and every count is taken from it.
  const whole = $derived(store.baseline ?? store.view);
  const columns = $derived(view?.settings.columns ?? []);
  const readonly = $derived(store.readonly);
  const mode = $derived<LaneMode | null>(lane === 'none' ? null : lane);

  const layoutOf = createLayout();
  const layout = $derived<Layout>(view ? layoutOf(view, lane) : { lanes: [], cells: new Map() });

  const colors = $derived(new Map((view?.settings.groups ?? []).map((g) => [g.name, groupColor(g.color)])));
  const topPriority = $derived(view?.settings.priorities[0] ?? 'P0');

  const totals = $derived.by(() => {
    const m = new Map<string, number>();
    for (const c of whole?.cards ?? []) if (!c.archived) m.set(c.column, (m.get(c.column) ?? 0) + 1);
    return m;
  });

  const laneStats = $derived.by(() => {
    const m = new Map<string, { done: number; total: number }>();
    if (!whole || !mode) return m;
    const done = new Set(whole.settings.columns.filter((c) => c.done).map((c) => c.name));
    for (const c of whole.cards) {
      if (c.archived) continue;
      const key = laneKeyOf(c, mode);
      const s = m.get(key) ?? { done: 0, total: 0 };
      s.total++;
      if (done.has(c.column)) s.done++;
      m.set(key, s);
    }
    return m;
  });

  // ----- Collapsed lanes, remembered per project -----
  let collapsed = $state.raw<ReadonlySet<string>>(new Set());

  $effect.pre(() => {
    collapsed = mode ? loadCollapsed(project, mode) : new Set();
  });

  function toggleLane(key: string) {
    if (!mode) return;
    const next = new Set(collapsed);
    if (!next.delete(key)) next.add(key);
    collapsed = next;
    saveCollapsed(project, mode, next);
  }

  // ----- What the keyboard sees -----
  /** The visible cards of each column, top to bottom. Built when a key needs it. */
  function navGrid(): NavGrid {
    return columns.map((col) => {
      if (!mode) return (layout.cells.get(zoneId(col.name)) ?? []).map((c) => ({ id: c.id, lane: '' }));
      return layout.lanes
        .filter((l) => !collapsed.has(l.key))
        .flatMap((l) => (layout.cells.get(zoneId(col.name, l.key)) ?? []).map((c) => ({ id: c.id, lane: l.key })));
    });
  }

  // The one card in the tab order: the one last focused, else the first.
  let rover = $state<string | null>(null);
  const tabbableId = $derived.by(() => {
    if (!view) return null;
    if (rover !== null && view.cards.some((c) => c.id === rover)) return rover;
    return firstVisible();
  });

  function firstVisible(): string | null {
    for (const col of columns) {
      if (!mode) {
        const first = layout.cells.get(zoneId(col.name))?.[0];
        if (first) return first.id;
        continue;
      }
      for (const l of layout.lanes) {
        if (collapsed.has(l.key)) continue;
        const first = layout.cells.get(zoneId(col.name, l.key))?.[0];
        if (first) return first.id;
      }
    }
    return null;
  }

  let boardEl: HTMLElement | undefined = $state();

  function cardEl(id: string): HTMLElement | null {
    if (!boardEl) return null;
    for (const el of boardEl.querySelectorAll<HTMLElement>('[data-card-id]')) if (el.dataset.cardId === id) return el;
    return null;
  }

  /** Focus a card once the board has been drawn. A moved card is a new element, so it has to be found again. */
  async function focusCard(id: string) {
    await tick();
    cardEl(id)?.focus({ preventScroll: false });
  }

  // The focused card and its neighbours, so focus can be kept when the board changes under it.
  let focusCtx: { id: string; near: string[] } | null = null;

  function noteFocus(id: string) {
    const grid = navGrid();
    const near: string[] = [];
    for (const dir of ['up', 'down', 'left', 'right'] as const) {
      const to = focusTarget(grid, id, dir);
      if (to !== null) near.push(to);
    }
    focusCtx = { id, near };
  }

  function onFocusIn(e: FocusEvent) {
    const id = e.target instanceof HTMLElement ? e.target.dataset.cardId : undefined;
    if (id === undefined) return;
    rover = id;
    noteFocus(id);
  }

  function onFocusOut(e: FocusEvent) {
    const el = e.target;
    if (!(el instanceof HTMLElement) || el.dataset.cardId === undefined) return;
    // A card that is rebuilt loses focus too, but it is gone from the page by now. One that is still
    // there and no longer focused was left on purpose, and nothing may take focus back for it.
    queueMicrotask(() => {
      if (el.isConnected && !boardEl?.contains(document.activeElement)) focusCtx = null;
    });
  }

  /** After the board is drawn: a focused card that was rebuilt or moved gets focus back, or its nearest neighbour does. */
  function restoreFocus() {
    const ctx = focusCtx;
    if (!ctx) return;
    const active = document.activeElement;
    if (active && active !== document.body && active.isConnected) {
      if (active instanceof HTMLElement && active.dataset.cardId !== undefined && boardEl?.contains(active)) noteFocus(active.dataset.cardId);
      return; // focus is somewhere, and is not ours to move
    }
    for (const id of [ctx.id, ...ctx.near]) {
      const el = cardEl(id);
      if (!el) continue;
      el.focus();
      noteFocus(id);
      return;
    }
  }

  $effect(() => {
    void layout;
    void collapsed;
    untrack(restoreFocus);
  });

  // ----- Announcements for screen readers -----
  let announcement = $state('');
  let flip = false;
  function announce(text: string) {
    flip = !flip;
    announcement = flip ? text : `${text} `; // the same words twice still read twice
  }

  // ----- Moving a card, by any route -----
  /**
   * Puts a card in a column at an index of the column's FULL rank order. In
   * swimlanes `laneKey` is the lane it is dropped in; a different lane first
   * changes the card's group (or priority), and only if that works does the
   * move run. Both go through the store's queue, in order.
   */
  async function relocate(id: string, column: string, index: number, laneKey?: string) {
    const card = whole?.cards.find((c) => c.id === id);
    if (!card || readonly) return;
    let laneName = '';
    if (mode && laneKey !== undefined && laneKeyOf(card, mode) !== laneKey) {
      laneName = laneKey === '' ? (mode === 'group' ? 'no group' : 'no priority') : laneKey;
      try {
        await store.patch(id, mode === 'group' ? { group: laneKey } : { priority: laneKey });
      } catch {
        // The store has shown the reason. The card stays where it is, and is not moved.
        void focusCard(id);
        return;
      }
    }

    const now = store.baseline ?? store.view;
    const ids = now ? columnIds(now.cards, column) : [];
    const here = now?.cards.find((c) => c.id === id);
    const unchanged = here !== undefined && here.column === column && ids.indexOf(id) === index;
    if (unchanged) {
      if (laneName !== '') announce(`${id} moved to ${laneName}`);
      void focusCard(id);
      return;
    }

    if (here && here.column === column) {
      announce(`${id} moved to position ${Math.min(index, ids.length - 1) + 1} of ${ids.length} in ${column}`);
    } else {
      announce(`${id} moved to ${column}${laneName === '' ? '' : `, ${laneName}`}`);
    }
    const moved = store.move(id, column, index).catch(() => {});
    void focusCard(id);
    await moved;
    // A move the server refused puts the card back; keep the keyboard on it.
    await tick();
    if (!boardEl?.contains(document.activeElement)) void focusCard(id);
  }

  // ----- Dragging -----
  let dragState = $state.raw<DragState | null>(null);
  // Cards read only this, which changes when a drag starts and ends, not as it moves.
  const dragId = $derived(dragState?.cardId ?? null);

  const drag = createDrag({
    canDrop: (id, column) => (whole ? canDrop(whole.settings, whole.cards, id, column) : 'The board has not loaded'),
    onDrop(id, column, _visibleIndex, target: DropTarget) {
      if (!whole) return;
      // The index is where the card goes among ALL the column's cards, not just those the filter shows.
      void relocate(id, column, dropIndex(columnIds(whole.cards, column), id, target.after, target.before), target.lane);
    },
    onChange: (s) => (dragState = s),
  });

  // The board refreshed while a card is in hand: what was measured is stale, and the card may be gone.
  $effect(() => {
    void layout;
    const present = whole;
    untrack(() => {
      if (!drag.active) return;
      const id = dragId;
      if (id !== null && present && !present.cards.some((c) => c.id === id && !c.archived)) {
        drag.cancel();
        toasts.push(`${id} was removed`);
        return;
      }
      void tick().then(() => drag.invalidate()); // once the new cards are on the page
    });
  });

  // A board that goes away mid-drag (another project was chosen) must not leave its copy of the card behind.
  $effect(() => () => drag.cancel());

  function onPointerDown(e: PointerEvent, id: string, el: HTMLElement) {
    if (readonly) return;
    drag.begin(e, id, el);
  }

  // ----- Opening a card -----
  function open(id: string) {
    navigate(href({ name: 'card', project, id, search: '' }));
  }

  // ----- The keyboard -----
  function refusal(id: string, column: string): string | null {
    return whole ? canDrop(whole.settings, whole.cards, id, column) : 'The board has not loaded';
  }

  function keyboardMove(id: string, dir: 'up' | 'down' | 'left' | 'right') {
    if (!whole) return;
    const card = whole.cards.find((c) => c.id === id);
    if (!card) return;
    if (dir === 'up' || dir === 'down') {
      const to = stepTarget(navGrid(), id, dir);
      if (!to) return;
      void relocate(id, card.column, dropIndex(columnIds(whole.cards, card.column), id, to.after, to.before));
      return;
    }
    const names = columns.map((c) => c.name);
    const dest = adjacentColumn(names, card.column, dir, (name) => refusal(id, name));
    if (dest === null) {
      const next = names[names.indexOf(card.column) + (dir === 'left' ? -1 : 1)];
      announce(next === undefined ? `${id} is already in the ${dir === 'left' ? 'first' : 'last'} column` : (refusal(id, next) ?? `${id} cannot move ${dir}`));
      return;
    }
    // The same height in the next column, as far as it goes.
    const here = columnIds(whole.cards, card.column).indexOf(id);
    void relocate(id, dest, Math.min(here, columnIds(whole.cards, dest).length));
  }

  let menu = $state.raw<{ id: string; anchor: { left: number; top: number } } | null>(null);

  const menuColumns = $derived.by(() => {
    if (!menu || !whole) return [];
    const card = whole.cards.find((c) => c.id === menu!.id);
    if (!card) return [];
    return columns.filter((c) => c.name !== card.column && refusal(card.id, c.name) === null).map((c) => c.name);
  });

  const menuLanes = $derived.by(() => {
    if (!menu || !whole || !mode) return [];
    const card = whole.cards.find((c) => c.id === menu!.id);
    if (!card) return [];
    const own = laneKeyOf(card, mode);
    return layout.lanes.filter((l) => l.key !== own).map((l) => ({ key: l.key, name: l.name }));
  });

  function openMenu(id: string, el: HTMLElement) {
    const r = el.getBoundingClientRect();
    menu = { id, anchor: { left: r.left, top: r.top } };
  }

  function closeMenu(refocus = true) {
    const id = menu?.id;
    menu = null;
    if (refocus && id !== undefined) void focusCard(id);
  }

  function menuMove(column: string | null, where: 'top' | 'bottom') {
    const id = menu?.id;
    const card = whole?.cards.find((c) => c.id === id);
    closeMenu(false);
    if (!whole || !card || id === undefined) return;
    const target = column ?? card.column;
    const n = columnIds(whole.cards, target).length;
    const bottom = target === card.column ? n - 1 : n; // not counting the card itself
    void relocate(id, target, where === 'top' ? 0 : bottom);
  }

  function menuLane(key: string) {
    const id = menu?.id;
    const card = whole?.cards.find((c) => c.id === id);
    closeMenu(false);
    if (!whole || !card || id === undefined) return;
    // Its place in the column does not change, so this is the group (or priority) change and nothing else.
    void relocate(id, card.column, columnIds(whole.cards, card.column).indexOf(id), key);
  }

  function onKeydown(e: KeyboardEvent) {
    if (menu || drag.active) return;
    const el = e.target instanceof HTMLElement ? e.target : null;
    const id = el?.dataset.cardId;
    if (!el || id === undefined) return;
    const action = readKey(e);
    if (!action) return;
    e.preventDefault();
    switch (action.kind) {
      case 'focus': {
        const to = focusTarget(navGrid(), id, action.dir);
        if (to !== null) void focusCard(to);
        break;
      }
      case 'move':
        if (!readonly) keyboardMove(id, action.dir);
        break;
      case 'menu':
        if (!readonly) openMenu(id, el);
        break;
      case 'open':
        open(id);
        break;
    }
  }
</script>

<!-- The board takes keys from the cards inside it, which are the focusable things. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="board"
  class:lanes={mode !== null}
  role="group"
  aria-label="Board"
  aria-busy={view === null}
  data-board
  data-scroll-y={mode !== null ? '' : undefined}
  bind:this={boardEl}
  style:--n={columns.length}
  onkeydown={onKeydown}
  onfocusin={onFocusIn}
  onfocusout={onFocusOut}
>
  {#if view}
    {#if mode === null}
      <div class="track">
        {#each columns as col (col.name)}
          {@const zone = zoneId(col.name)}
          {@const cards = layout.cells.get(zone) ?? []}
          <Column
            part="full"
            column={col}
            {cards}
            total={totals.get(col.name) ?? 0}
            {zone}
            {colors}
            {topPriority}
            {tabbableId}
            {dragId}
            refusal={dragState?.blocked.get(col.name) ?? null}
            explain={dragState?.over === zone}
            slot={dragState?.target?.zone === zone ? dragState.target.slotY : null}
            {readonly}
            onAdd={onAddCard}
            onOpen={open}
            {onPointerDown}
          />
        {/each}
      </div>
    {:else}
      <Swimlanes
        {mode}
        {layout}
        {columns}
        {totals}
        stats={laneStats}
        {collapsed}
        {colors}
        {topPriority}
        {tabbableId}
        {dragId}
        drag={dragState}
        {readonly}
        onToggle={toggleLane}
        onOpen={open}
        {onPointerDown}
      />
    {/if}
  {/if}
</div>

<div class="sr-only" role="log" aria-label="Moves" aria-live="polite" aria-atomic="true">{announcement}</div>

{#if menu}
  <MoveMenu
    cardId={menu.id}
    columns={menuColumns}
    lanes={menuLanes}
    anchor={menu.anchor}
    onColumn={(c) => menuMove(c, 'bottom')}
    onLane={menuLane}
    onTop={() => menuMove(null, 'top')}
    onBottom={() => menuMove(null, 'bottom')}
    onClose={() => closeMenu()}
  />
{/if}

<style>
  .board {
    --col-min: 250px;
    flex: 1;
    min-width: 0;
    min-height: 0;
    /* The inset gives the first and last columns room for their focus rings and hard edges. */
    margin-inline: -8px;
    padding-inline: 8px;
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    overflow-x: auto;
    overflow-y: hidden;
  }

  .board.lanes {
    display: block;
    overflow-y: auto;
  }

  .track {
    display: grid;
    grid-template-columns: repeat(var(--n), minmax(var(--col-min), 1fr));
    gap: var(--col-gap);
    min-height: 0;
  }

  @media (max-width: 560px) {
    .board {
      --col-min: min(272px, 82vw);
    }
  }

  :global(body.wp-dragging) {
    cursor: grabbing;
    user-select: none;
    -webkit-user-select: none;
  }
</style>
