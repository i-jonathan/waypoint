<script lang="ts">
  import Columns from 'phosphor-svelte/lib/Columns';
  import List from 'phosphor-svelte/lib/List';
  import MagnifyingGlass from 'phosphor-svelte/lib/MagnifyingGlass';
  import Plus from 'phosphor-svelte/lib/Plus';
  import Rows from 'phosphor-svelte/lib/Rows';
  import type { Swimlane } from '../lib/types';

  interface Props {
    /** False when no board is on screen: only the menu button shows. */
    active: boolean;
    /** False on Next up, Blocked and Archive: the filter and the swimlane toggle belong to the board. */
    filterable?: boolean;
    query: string;
    lane: Swimlane;
    /** While true, New card is disabled. */
    readonly: boolean;
    menuOpen: boolean;
    onQuery: (q: string) => void;
    onLane: (lane: Swimlane) => void;
    onNewCard: () => void;
    onMenu: () => void;
  }

  let { active, filterable = true, query, lane, readonly, menuOpen, onQuery, onLane, onNewCard, onMenu }: Props = $props();

  let input: HTMLInputElement | undefined = $state();

  function typing(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false;
    return target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
  }

  function onKey(e: KeyboardEvent) {
    if (!active || e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
    if (e.key === '/') {
      if (!filterable) return;
      e.preventDefault();
      input?.focus();
    } else if (e.key === 'n' || e.key === 'N') {
      if (readonly) return;
      e.preventDefault();
      onNewCard();
    }
  }

  function onInputKey(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    if (query !== '') onQuery('');
    else input?.blur();
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="bar">
  <button
    id="menu-button"
    class="icon menu"
    aria-label="Menu"
    title="Menu"
    aria-controls="sidebar"
    aria-expanded={menuOpen}
    onclick={onMenu}
  >
    <List size={18} aria-hidden="true" />
  </button>

  {#if active}
    {#if filterable}
      <div class="q-wrap" role="search">
        <label class="q" class:filled={query !== ''}>
          <MagnifyingGlass size={15} aria-hidden="true" />
          <input
            bind:this={input}
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder="Filter"
            aria-label="Filter cards"
            aria-describedby="filter-hint"
            aria-keyshortcuts="/"
            value={query}
            oninput={(e) => onQuery(e.currentTarget.value)}
            onkeydown={onInputKey}
          />
          <code id="filter-hint" class="hint">p0 blocked group:security</code>
        </label>
      </div>

      <div class="seg" role="group" aria-label="View">
        <button class:on={lane === 'none'} aria-pressed={lane === 'none'} aria-label="Board" title="Board" onclick={() => onLane('none')}>
          <Columns size={15} aria-hidden="true" />
        </button>
        <button
          class:on={lane !== 'none'}
          aria-pressed={lane !== 'none'}
          aria-label="Swimlanes"
          title="Swimlanes"
          onclick={() => lane === 'none' && onLane('group')}
        >
          <Rows size={15} aria-hidden="true" />
        </button>
      </div>

      {#if lane !== 'none'}
        <select
          class="lane"
          aria-label="Swimlanes by"
          value={lane}
          onchange={(e) => onLane(e.currentTarget.value as Swimlane)}
        >
          <option value="group">Group</option>
          <option value="priority">Priority</option>
        </select>
      {/if}
    {:else}
      <div class="grow"></div>
    {/if}

    <button
      class="go"
      disabled={readonly}
      title={readonly ? 'The board is read-only until the file is fixed' : 'New card'}
      aria-keyshortcuts="N"
      onclick={onNewCard}
    >
      <Plus size={15} weight="bold" aria-hidden="true" />
      <span class="go-label">New card</span>
      <kbd aria-hidden="true">N</kbd>
    </button>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
  }

  .grow {
    flex: 1;
  }

  .q-wrap {
    flex: 1;
    min-width: 140px;
    display: flex;
  }

  .q {
    flex: 1;
    min-width: 0;
    display: flex;
    gap: 8px;
    align-items: center;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
    color: var(--muted);
    cursor: text;
  }

  .q:focus-within {
    border-color: var(--acc);
    outline: 2px solid var(--acc);
    outline-offset: 2px;
    box-shadow: 0 0 0 2px var(--text);
  }

  /* The input's own ring would double the box's. */
  input:focus-visible {
    outline: 0;
    box-shadow: none;
  }

  .q :global(svg) {
    flex: none;
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    font-size: 13px;
    color: var(--text);
  }

  input::placeholder {
    color: var(--muted);
    opacity: 1;
  }

  /* Unfocused and empty, the box reads "Filter p0 blocked group:security": the input
     shrinks to the width of its placeholder and the syntax hint sits after it. */
  .q:not(:focus-within):not(.filled) input {
    flex: none;
    width: 6ch;
  }

  .hint {
    font-family: var(--font-mono);
    font-size: 11px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .q:focus-within .hint,
  .q.filled .hint {
    display: none;
  }

  .seg {
    display: flex;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    overflow: hidden;
    background: var(--card);
  }

  .seg button {
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 36px;
    min-height: 32px;
    padding: 0 10px;
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .seg button:hover {
    color: var(--text);
  }

  .seg button.on {
    background: var(--head);
    color: var(--headtext);
  }

  /* The group clips its edge, so its ring is drawn inside: text colour on the edge, accent within.
     The filled button has the text colour as its fill, so its edge ring uses the canvas colour. */
  .seg button:focus-visible {
    outline-offset: -4px;
    box-shadow: inset 0 0 0 2px var(--text);
  }

  .seg button.on:focus-visible {
    box-shadow: inset 0 0 0 2px var(--bg);
  }

  .lane {
    min-height: 32px;
    padding: 0 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
  }

  .go {
    display: flex;
    gap: 7px;
    align-items: center;
    min-height: 32px;
    padding: 8px 14px;
    border: 0;
    border-radius: var(--r-control);
    background: var(--acc);
    color: var(--acc-ink);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .go:hover:not(:disabled) {
    filter: brightness(1.06);
  }

  .go:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  kbd {
    margin-left: 2px;
    padding: 0 5px;
    border: 1px solid color-mix(in srgb, currentColor 40%, transparent);
    border-radius: var(--r-chip);
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 500;
    line-height: 16px;
    opacity: 0.8;
  }

  .icon {
    display: none;
    align-items: center;
    justify-content: center;
    min-width: 36px;
    min-height: 32px;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    cursor: pointer;
  }

  @media (max-width: 719px) {
    .icon.menu {
      display: flex;
    }
  }

  @media (max-width: 560px) {
    .hint,
    kbd {
      display: none;
    }

    .q:not(:focus-within):not(.filled) input {
      flex: 1;
      width: auto;
    }

    .go-label {
      /* Icon only: the button keeps its name through the title and the visible text for assistive tech */
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }

    .go {
      padding: 8px 10px;
    }
  }
</style>
