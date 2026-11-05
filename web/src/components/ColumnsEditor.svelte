<script lang="ts">
  import { tick } from 'svelte';
  import ArrowDown from 'phosphor-svelte/lib/ArrowDown';
  import ArrowUp from 'phosphor-svelte/lib/ArrowUp';
  import DotsSixVertical from 'phosphor-svelte/lib/DotsSixVertical';
  import Info from 'phosphor-svelte/lib/Info';
  import Plus from 'phosphor-svelte/lib/Plus';
  import Trash from 'phosphor-svelte/lib/Trash';
  import { addColumn, moveRow, type ColumnRow, type Errors } from '../lib/settingsEdit';

  interface Props {
    rows: ColumnRow[];
    /** How many cards each column holds now, by the name it has on the board. */
    counts: Record<string, number>;
    errors: Errors;
  }

  let { rows = $bindable(), counts, errors }: Props = $props();

  let list: HTMLElement | undefined = $state();
  let announcement = $state('');

  const nameOf = (r: ColumnRow, i: number) => r.name.trim() || `column ${i + 1}`;
  const cardsIn = (r: ColumnRow) => (r.origin === null ? 0 : (counts[r.origin] ?? 0));

  /** Why a column cannot be removed, or null when it can. */
  function keepReason(r: ColumnRow): string | null {
    const n = cardsIn(r);
    if (n > 0) return `Move its ${n} ${n === 1 ? 'card' : 'cards'} out first`;
    if (rows.length <= 1) return 'At least one column must remain';
    return null;
  }

  function toggleMove(r: ColumnRow, key: string, on: boolean) {
    r.moves = on ? [...r.moves.filter((k) => k !== key), key] : r.moves.filter((k) => k !== key);
  }

  function remove(r: ColumnRow) {
    if (keepReason(r) !== null) return;
    rows = rows.filter((o) => o.key !== r.key).map((o) => ({ ...o, moves: o.moves.filter((k) => k !== r.key) }));
  }

  const rowEl = (key: string) => list?.querySelector<HTMLElement>(`[data-key="${key}"]`) ?? null;

  /** Moves a row, says so, and keeps focus on a move button that still works. */
  async function shift(from: number, to: number, via: 'up' | 'down' | null) {
    if (to < 0 || to >= rows.length || from === to) return;
    const r = rows[from]!;
    rows = moveRow(rows, from, to);
    announcement = `${nameOf(r, to)} moved to position ${to + 1} of ${rows.length}`;
    if (via === null) return;
    await tick();
    const el = rowEl(r.key);
    const wanted = el?.querySelector<HTMLButtonElement>(`[data-move="${via}"]`);
    (wanted && !wanted.disabled ? wanted : el?.querySelector<HTMLButtonElement>('[data-move]:not(:disabled)'))?.focus();
  }

  async function add() {
    rows = addColumn(rows);
    const added = rows[rows.length - 1]!;
    announcement = `${added.name} added as column ${rows.length}`;
    await tick();
    const input = document.getElementById(`col-${added.key}-name`) as HTMLInputElement | null;
    input?.focus();
    input?.select();
  }

  // ----- Drag: a pointer on the handle, a line where the row would land. -----
  let dragging = $state<{ key: string; from: number; at: number } | null>(null);

  function begin(e: PointerEvent, i: number) {
    if (e.button !== 0) return;
    e.preventDefault();
    dragging = { key: rows[i]!.key, from: i, at: i };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', stop);
    window.addEventListener('keydown', onKey);
  }

  /** The place a row dropped at this height would go: the index of the row it lands before. */
  function slotAt(y: number): number {
    const items = rows.map((r) => rowEl(r.key));
    for (let i = 0; i < items.length; i++) {
      const box = items[i]?.getBoundingClientRect();
      if (box && y < box.top + box.height / 2) return i;
    }
    return items.length;
  }

  function onMove(e: PointerEvent) {
    if (dragging) dragging = { ...dragging, at: slotAt(e.clientY) };
  }

  function onUp() {
    const d = dragging;
    stop();
    if (!d) return;
    const to = d.at > d.from ? d.at - 1 : d.at;
    void shift(d.from, to, null);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') stop();
  }

  function stop() {
    dragging = null;
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', stop);
    window.removeEventListener('keydown', onKey);
  }
</script>

<div class="cols" class:dragging={dragging !== null}>
  <ol bind:this={list}>
    {#each rows as r, i (r.key)}
      {@const reason = keepReason(r)}
      {@const nameErr = errors[`col-${r.key}-name`]}
      {@const wipErr = errors[`col-${r.key}-wip`]}
      <li
        data-key={r.key}
        class:lifted={dragging?.key === r.key}
        class:before={dragging !== null && dragging.at === i && dragging.at !== dragging.from && dragging.at !== dragging.from + 1}
        class:after={dragging !== null && i === rows.length - 1 && dragging.at === rows.length && dragging.from !== rows.length - 1}
      >
        <div class="row" role="group" aria-labelledby="col-{r.key}-head">
          <div class="head">
            <span class="grip" aria-hidden="true" title="Drag to reorder" onpointerdown={(e) => begin(e, i)}>
              <DotsSixVertical size={18} weight="bold" />
            </span>
            <span class="num mono" id="col-{r.key}-head">Column {i + 1}</span>
            <span class="grow"></span>
            <button
              type="button"
              class="sf-icon-btn"
              data-move="up"
              aria-label="Move column {nameOf(r, i)} up"
              title="Move up"
              disabled={i === 0}
              onclick={() => shift(i, i - 1, 'up')}
            >
              <ArrowUp size={14} weight="bold" aria-hidden="true" />
            </button>
            <button
              type="button"
              class="sf-icon-btn"
              data-move="down"
              aria-label="Move column {nameOf(r, i)} down"
              title="Move down"
              disabled={i === rows.length - 1}
              onclick={() => shift(i, i + 1, 'down')}
            >
              <ArrowDown size={14} weight="bold" aria-hidden="true" />
            </button>
            <button
              type="button"
              class="sf-icon-btn"
              aria-label="Remove column {nameOf(r, i)}"
              title={reason ?? 'Remove column'}
              disabled={reason !== null}
              onclick={() => remove(r)}
            >
              <Trash size={14} aria-hidden="true" />
            </button>
          </div>

          <div class="grid">
            <div class="sf-field">
              <label class="sf-label" for="col-{r.key}-name">Name</label>
              <input
                id="col-{r.key}-name"
                class="sf-input"
                type="text"
                bind:value={r.name}
                aria-invalid={nameErr ? 'true' : undefined}
                aria-describedby={nameErr ? `col-${r.key}-name-err` : undefined}
              />
              {#if nameErr}<p class="sf-error" id="col-{r.key}-name-err">{nameErr}</p>{/if}
            </div>

            <div class="sf-field wip">
              <label class="sf-label" for="col-{r.key}-wip">WIP limit</label>
              <input
                id="col-{r.key}-wip"
                class="sf-input mono"
                type="text"
                inputmode="numeric"
                placeholder="None"
                bind:value={r.wip}
                aria-invalid={wipErr ? 'true' : undefined}
                aria-describedby={wipErr ? `col-${r.key}-wip-err` : undefined}
              />
              {#if wipErr}<p class="sf-error" id="col-{r.key}-wip-err">{wipErr}</p>{/if}
            </div>

            <div class="sf-field done">
              <span class="sf-label" aria-hidden="true">Done</span>
              <label class="sf-check">
                <input type="checkbox" bind:checked={r.done} />
                <span>Counts as done</span>
              </label>
            </div>
          </div>

          <fieldset class="moves">
            <legend class="sf-label">Can move to</legend>
            <div class="boxes">
              {#each rows as o (o.key)}
                {#if o.key !== r.key}
                  <label class="sf-check">
                    <input type="checkbox" checked={r.moves.includes(o.key)} onchange={(e) => toggleMove(r, o.key, e.currentTarget.checked)} />
                    <span>{o.name.trim() || 'Unnamed'}</span>
                  </label>
                {/if}
              {/each}
              {#if rows.length === 1}<span class="sf-hint">There is no other column yet.</span>{/if}
            </div>
            {#if r.moves.length === 0}
              <p class="soft"><Info size={13} aria-hidden="true" /><span>Cards can't leave this column</span></p>
            {/if}
          </fieldset>
        </div>
      </li>
    {/each}
  </ol>

  <button type="button" class="btn add" onclick={add}><Plus size={14} weight="bold" aria-hidden="true" />Add column</button>
  <div class="sr-only" role="status" aria-live="polite">{announcement}</div>
</div>

<style>
  .cols.dragging {
    user-select: none;
    cursor: grabbing;
  }

  .cols {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  ol {
    list-style: none;
    margin: 0;
    padding: 0;
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  li {
    position: relative;
  }

  li.lifted {
    opacity: 0.5;
  }

  li.before::before,
  li.after::after {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    height: 3px;
    border-radius: 2px;
    background: var(--acc);
  }

  li.before::before {
    top: -7px;
  }

  li.after::after {
    bottom: -7px;
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 12px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: color-mix(in srgb, var(--bg) 40%, var(--card));
  }

  .head {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .grip {
    display: inline-grid;
    place-items: center;
    width: 24px;
    height: 28px;
    color: var(--muted);
    cursor: grab;
    touch-action: none;
  }

  .num {
    font-size: 11px;
    color: var(--muted);
  }

  .grow {
    flex: 1;
  }

  .grid {
    display: grid;
    grid-template-columns: minmax(160px, 1fr) 120px auto;
    gap: 12px 16px;
    align-items: start;
  }

  .wip {
    max-width: 120px;
  }

  .done {
    padding-top: 0;
  }

  .done .sf-label {
    visibility: hidden;
  }

  .moves {
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }

  .moves legend {
    padding: 0;
    margin-bottom: 4px;
  }

  .boxes {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 18px;
  }

  .soft {
    display: flex;
    gap: 6px;
    align-items: center;
    margin: 6px 0 0;
    font-size: 12px;
    color: var(--muted);
  }

  .add {
    align-self: flex-start;
  }

  @media (max-width: 560px) {
    .grid {
      grid-template-columns: 1fr;
    }

    .wip {
      max-width: none;
    }

    .done .sf-label {
      display: none;
    }
  }
</style>
