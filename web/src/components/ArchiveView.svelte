<script lang="ts">
  import { tick, untrack } from 'svelte';
  import Archive from 'phosphor-svelte/lib/Archive';
  import ArrowCounterClockwise from 'phosphor-svelte/lib/ArrowCounterClockwise';
  import MagnifyingGlass from 'phosphor-svelte/lib/MagnifyingGlass';
  import { api } from '../lib/api';
  import type { BoardStore } from '../lib/board.svelte';
  import { groupColor } from '../lib/colors';
  import { displayDate } from '../lib/dates';
  import { toasts } from '../lib/events.svelte';
  import { go } from '../lib/link';
  import { href, navigate } from '../lib/router.svelte';
  import type { CardView } from '../lib/types';
  import { archiveMonths, doneCount } from '../lib/views';
  import Card from './Card.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import Notice from './Notice.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    /** The archive route. A test passes a fake. */
    client?: Pick<typeof api, 'archive'>;
    /** How long typing pauses before the search is sent. */
    debounceMs?: number;
  }

  let { store, project, client = api, debounceMs = 200 }: Props = $props();

  const whole = $derived(store.baseline ?? store.view);
  const columns = $derived(whole?.settings.columns ?? []);
  const colors = $derived(new Map((whole?.settings.groups ?? []).map((g) => [g.name, groupColor(g.color)])));
  const topPriority = $derived(whole?.settings.priorities[0] ?? 'P0');
  const readonly = $derived(store.readonly);

  // ----- The archive: its own list, fetched here -----
  let typed = $state(''); // what the box holds; a reload never rewrites it
  let applied = $state(''); // what was last sent, after the pause
  let cards = $state.raw<CardView[] | null>(null);
  let failed = $state(false);
  let seq = 0;
  let pause: ReturnType<typeof setTimeout> | undefined;

  function onInput(value: string) {
    typed = value;
    clearTimeout(pause);
    pause = setTimeout(() => (applied = typed), debounceMs);
  }

  function clearSearch() {
    clearTimeout(pause);
    typed = '';
    applied = '';
    searchBox?.focus();
  }

  async function load(q: string) {
    const mine = ++seq;
    try {
      const list = await client.archive(project, q === '' ? undefined : q);
      if (mine !== seq) return;
      cards = list;
      failed = false;
    } catch (e) {
      if (mine !== seq) return;
      failed = true;
      toasts.push(e instanceof Error && e.message !== '' ? e.message : 'Could not load the archive');
    }
  }

  // Loads on arrival, when the search is sent, and when the server reports a change to this project.
  $effect(() => {
    const q = applied;
    void store.changes;
    untrack(() => void load(q));
  });

  $effect(() => () => {
    seq++; // answers still on their way are dropped
    clearTimeout(pause);
  });

  const months = $derived(cards ? archiveMonths(cards) : []);

  // ----- Opening and focus -----
  const cardHref = (id: string) => href({ name: 'card', project, id, search: '' });

  function open(id: string) {
    navigate(cardHref(id));
  }

  /** Cards are not in the tab order here: each row has an Open button. Enter and Space still open a focused one. */
  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const id = e.target instanceof HTMLElement ? e.target.dataset.cardId : undefined;
    if (id === undefined) return;
    e.preventDefault();
    open(id);
  }

  let heading: HTMLElement | undefined = $state();
  let searchBox: HTMLInputElement | undefined = $state();
  let root: HTMLElement | undefined = $state();

  // Focus lands on the heading when the view opens.
  $effect(() => {
    heading?.focus({ preventScroll: true });
  });

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && typed !== '') {
      e.preventDefault();
      clearSearch();
    }
  }

  // ----- Restore -----
  const columnId = `restore-col-${Math.random().toString(36).slice(2, 8)}`;
  let pick = $state('');
  // The first column unless one was picked; a column renamed away falls back to the first.
  const target = $derived(columns.some((c) => c.name === pick) ? pick : (columns[0]?.name ?? ''));
  let restoring = $state.raw<ReadonlySet<string>>(new Set());

  function setBusy(id: string, on: boolean) {
    const next = new Set(restoring);
    if (on) next.add(id);
    else next.delete(id);
    restoring = next;
  }

  async function restore(c: CardView) {
    if (restoring.has(c.id) || readonly || target === '') return;
    const column = target;
    // Its place in the list as drawn, which is by month, not in the order the server sent.
    const at = months.flatMap((m) => m.cards).findIndex((x) => x.id === c.id);
    setBusy(c.id, true);
    try {
      await store.restore(c.id, column);
    } catch {
      // The store has shown the server's message. The card stays listed.
      setBusy(c.id, false);
      return;
    }
    setBusy(c.id, false);
    cards = (cards ?? []).filter((x) => x.id !== c.id);
    toasts.push(`Restored ${c.id} to ${column}`, 'info');
    // The row is gone: keep the keyboard in the list, on the card that took its place.
    await tick();
    const buttons = root?.querySelectorAll<HTMLElement>('[data-restore]') ?? [];
    (buttons[Math.min(Math.max(at, 0), buttons.length - 1)] ?? heading)?.focus();
  }

  // ----- Archive done cards -----
  const done = $derived(whole ? doneCount(whole) : 0);
  const mode = $derived(whole?.settings.archive.mode ?? 'manual');
  const reason = $derived(readonly ? 'The board is read-only until the file is fixed.' : done === 0 ? 'No done cards to archive.' : '');
  const reasonId = `archive-reason-${Math.random().toString(36).slice(2, 8)}`;
  // The count the confirm was opened with, or null while it is closed.
  let confirming = $state<number | null>(null);

  const cardsOf = (n: number) => `${n} ${n === 1 ? 'card' : 'cards'}`;

  function askArchive() {
    if (reason !== '') return;
    confirming = done;
  }

  async function archiveDone() {
    confirming = null;
    try {
      const ids = await store.archiveDone();
      toasts.push(`Archived ${cardsOf(ids.length)}`, 'info');
      void load(applied);
    } catch {
      // The store has shown why.
    }
  }

  const boardHref = $derived(href({ name: 'board', project, search: '' }));
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<section class="view" bind:this={root} aria-busy={cards === null && !failed} onkeydown={onKeydown}>
  <header class="head">
    <h2 bind:this={heading} tabindex="-1">Archive</h2>
    {#if cards && cards.length > 0}<span class="count mono">{cards.length}</span>{/if}
  </header>

  <div class="tools">
    <label class="q">
      <MagnifyingGlass size={15} aria-hidden="true" />
      <input
        bind:this={searchBox}
        type="search"
        autocomplete="off"
        spellcheck="false"
        placeholder="Search the archive"
        aria-label="Search the archive"
        value={typed}
        oninput={(e) => onInput(e.currentTarget.value)}
        onkeydown={onSearchKey}
      />
    </label>

    {#if columns.length > 0}
      <div class="pick">
        <label for={columnId}>Restore to</label>
        <select id={columnId} value={target} onchange={(e) => (pick = e.currentTarget.value)}>
          {#each columns as col (col.name)}<option value={col.name}>{col.name}</option>{/each}
        </select>
      </div>
    {/if}

    {#if whole && mode !== 'off'}
      <div class="all">
        <button
          class="btn"
          aria-disabled={reason !== ''}
          aria-describedby={reason !== '' ? reasonId : undefined}
          onclick={askArchive}
        >
          <Archive size={15} aria-hidden="true" />Archive done cards
        </button>
        {#if reason !== ''}<span id={reasonId} class="why">{reason}</span>{/if}
      </div>
    {/if}
  </div>

  {#if cards === null}
    {#if failed}
      <Notice icon={Archive} title="The archive could not be read.">
        {#snippet actions()}
          <button class="btn" onclick={() => load(applied)}>Try again</button>
        {/snippet}
      </Notice>
    {/if}
  {:else if cards.length === 0}
    {#if applied === ''}
      <Notice icon={Archive} title="No archived cards yet.">
        {#snippet actions()}
          <a class="btn" href={boardHref} onclick={go}>Open the board</a>
        {/snippet}
      </Notice>
    {:else}
      <Notice icon={MagnifyingGlass} title="No archived cards match.">
        {#snippet actions()}
          <button class="btn" onclick={clearSearch}>Clear search</button>
        {/snippet}
      </Notice>
    {/if}
  {:else}
    {#each months as m (m.key)}
      <section class="month" data-month={m.key} aria-label={m.label}>
        <h3 class="sign">{m.label}</h3>
        <ul class="list">
          {#each m.cards as c (c.id)}
            <li data-archived={c.id}>
              <Card
                card={c}
                done
                top={c.priority !== '' && c.priority === topPriority}
                color={colors.get(c.group)}
                tabbable={false}
                dragging={false}
                drag={false}
                onOpen={open}
              />
              <div class="acts">
                <button
                  class="btn"
                  data-restore
                  aria-label="Restore {c.id}"
                  aria-disabled={readonly || restoring.has(c.id)}
                  title={readonly ? 'The board is read-only until the file is fixed' : `Restore to ${target}`}
                  onclick={() => restore(c)}
                >
                  <ArrowCounterClockwise size={14} weight="bold" aria-hidden="true" />Restore
                </button>
                <button class="btn" aria-label="Open {c.id}" onclick={() => open(c.id)}>Open</button>
                {#if c.done !== null}<span class="when mono">Done {displayDate(c.done)}</span>{/if}
              </div>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}
</section>

{#if confirming !== null}
  <ConfirmDialog
    title="Archive {confirming} done {confirming === 1 ? 'card' : 'cards'}?"
    message="They move to the archive section of the board file. Restore brings one back."
    confirmLabel="Archive"
    onConfirm={archiveDone}
    onCancel={() => (confirming = null)}
  />
{/if}

<style>
  .view {
    display: flex;
    flex-direction: column;
    gap: 14px;
    max-width: 640px;
    width: 100%;
    padding: 6px 0 24px;
  }

  .head {
    display: flex;
    gap: 10px;
    align-items: baseline;
  }

  h2 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  h2:focus {
    outline: none;
  }

  h2:focus-visible {
    outline: 2px solid var(--acc);
    outline-offset: 4px;
    box-shadow: none;
  }

  .count {
    font-size: 12px;
    color: var(--muted);
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 14px;
    align-items: center;
  }

  .q {
    flex: 1 1 200px;
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

  input:focus-visible {
    outline: 0;
    box-shadow: none;
  }

  input::placeholder {
    color: var(--muted);
    opacity: 1;
  }

  .pick {
    display: flex;
    gap: 8px;
    align-items: center;
    font-size: 12px;
    color: var(--muted);
  }

  select {
    min-height: 32px;
    padding: 0 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
  }

  .all {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 10px;
    align-items: center;
  }

  /* Off, but still reachable by keyboard, so the reason beside it can be read. */
  .btn[aria-disabled='true'] {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .why {
    font-size: 12px;
    color: var(--muted);
  }

  .month {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 6px;
  }

  .sign {
    align-self: flex-start;
    margin: 0;
    padding: 5px 10px 4px;
    border-radius: var(--r-sign);
    background: var(--head);
    color: var(--headtext);
    font-size: 13px;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 10px;
    align-items: center;
    margin: -2px 0 6px;
  }

  .acts .btn {
    min-height: 28px;
    padding: 4px 12px;
    font-size: 12px;
  }

  .when {
    font-size: 11px;
    color: var(--muted);
  }
</style>
